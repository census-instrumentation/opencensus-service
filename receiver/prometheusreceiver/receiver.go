// Copyright 2019, OpenCensus Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package prometheusreceiver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gokitLog "github.com/go-kit/kit/log"

	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery"
	sd_config "github.com/prometheus/prometheus/discovery/config"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/storage"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agentmetricspb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/metrics/v1"
	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
	"google.golang.org/api/support/bundler"
)

type metricsSink interface {
	ReceiveMetrics(context.Context, *agentmetricspb.ExportMetricsServiceRequest) error
}

func newCustomAppender(ms metricsSink, opts ...Option) *customAppender {
	ca := &customAppender{
		metricsSink: ms,
		metricsProcessor: &metricsProcessor{
			definitionsByMetricName: make(map[string][]*bucketDefinition),
			lastSeenData:            make(map[string]*lastSeenData),
		},
	}

	for _, opt := range opts {
		opt.withCustomAppender(ca)
	}

	allMetricsBundler := bundler.NewBundler((*bundling)(nil), func(data interface{}) {
		bundlings := data.([]*bundling)
		compactAndCombineMetrics(ms, bundlings)
	})
	allMetricsBundler.DelayThreshold = ca.metricsBufferPeriod
	if allMetricsBundler.DelayThreshold <= 0 {
		allMetricsBundler.DelayThreshold = 5 * time.Second // TODO: Make this configurable.
	}
	if allMetricsBundler.BundleCountThreshold <= 0 {
		allMetricsBundler.BundleCountThreshold = 100 // TODO: Make this configurable.
	}
	ca.allMetricsBundler = allMetricsBundler

	if ca.logger == nil {
		ca.logger = gokitLog.NewNopLogger()
	}

	return ca
}

// Receiver
type preceiver struct {
	cancelOnce sync.Once
	cancelFn   func()
	flushFn    func()
	errsChan   <-chan error
}

func (r *preceiver) Flush() {
	r.flushFn()
}

func (r *preceiver) ErrsChan() <-chan error {
	return r.errsChan
}

var errAlreadyCancelled = errors.New("already cancelled")

func (r *preceiver) Cancel() error {
	var err = errAlreadyCancelled
	r.cancelOnce.Do(func() {
		err = nil
	})
	return err
}

func receiverFromConfig(ctx context.Context, ms metricsSink, cfg *config.Config, opts ...Option) (*preceiver, error) {
	capp := newCustomAppender(ms, opts...)
	scrapeManager := scrape.NewManager(capp.logger, capp)
	capp.scrapeManager = scrapeManager

	ctxScrape, cancelScrape := context.WithCancel(ctx)

	discoveryManagerScrape := discovery.NewManager(ctxScrape, capp.logger)
	go discoveryManagerScrape.Run()
	scrapeManager.ApplyConfig(cfg)

	// Run the scrape manager.
	syncConfig := make(chan bool)
	errsChan := make(chan error, 1)
	go func() {
		defer close(errsChan)

		<-time.After(100 * time.Millisecond)
		close(syncConfig)
		if err := scrapeManager.Run(discoveryManagerScrape.SyncCh()); err != nil {
			errsChan <- err
		}
	}()
	<-syncConfig
	// By this point we've given time to the scrape manager
	// to start applying its original configuration.

	discoveryCfg := make(map[string]sd_config.ServiceDiscoveryConfig)
	for _, scrapeConfig := range cfg.ScrapeConfigs {
		discoveryCfg[scrapeConfig.JobName] = scrapeConfig.ServiceDiscoveryConfig
	}

	// Now trigger the discovery notification to the scrape manager.
	discoveryManagerScrape.ApplyConfig(discoveryCfg)

	rcv := &preceiver{
		cancelFn: cancelScrape,
		errsChan: errsChan,
		flushFn:  capp.flush,
	}

	return rcv, nil
}

type customAppender struct {
	allMetricsBundler   *bundler.Bundler
	logger              gokitLog.Logger
	metricsBufferPeriod time.Duration
	metricsBufferCount  int
	scrapeManager       *scrape.Manager
	metricsProcessor    *metricsProcessor
	metricsSink         metricsSink
}

var _ scrape.Appendable = (*customAppender)(nil)

func (ca *customAppender) Appender() (storage.Appender, error) {
	return ca, nil
}

func (ca *customAppender) flush() {
	ca.allMetricsBundler.Flush()
}

func (ca *customAppender) Commit() error   { return nil }
func (ca *customAppender) Rollback() error { return nil }

func (ca *customAppender) Add(l labels.Labels, t int64, v float64) (uint64, error) {
	return uint64(t + 1), ca.processMetrics(l, 0, t, v)
}

func (ca *customAppender) AddFast(l labels.Labels, ref uint64, t int64, v float64) error {
	return ca.processMetrics(l, ref, t, v)
}

func (ca *customAppender) processMetrics(l labels.Labels, ref uint64, t int64, v float64) error {
	metricName, metadata, scheme, err := ca.parseMetadata(l)
	if err != nil {
		return err
	}

	switch metadataType := string(metadata.Type); strings.ToLower(metadataType) {
	case "gaugehistogram", "histogram":
		return ca.processHistogramLikeMetrics(metricName, metadata, scheme, l, ref, t, v)

	default:
		return ca.processNonHistogramLikeMetrics(metricName, metadata, scheme, l, ref, t, v)
	}
}

func (ca *customAppender) parseMetadata(labelsList labels.Labels) (string, *scrape.MetricMetadata, string, error) {
	var jobName, metricName string

	for _, label := range labelsList {
		switch label.Name {
		case labels.MetricName:
			metricName = label.Value
			// Don't waste time processing Prometheus scrape metrics.
			if metricName == "up" || strings.HasPrefix(metricName, "scrape_") {
				return "", nil, "", fmt.Errorf("%q is not an interesting metric", metricName)
			}

			switch {
			case strings.HasSuffix(metricName, "_count"):
				metricName = strings.TrimSuffix(metricName, "_count")

			case strings.HasSuffix(metricName, "_sum"):
				metricName = strings.TrimSuffix(metricName, "_sum")

			case strings.HasSuffix(metricName, "_bucket"):
				// Keep this metricName as is because, it'll be processed
				// later on and have its suffix trimmed if necessary.
			}
		case "job":
			jobName = label.Value

		case labels.BucketLabel:
			if len(metricName) > 0 {
				lastUnderscoreIndex := strings.LastIndex(metricName, "_")
				if lastUnderscoreIndex > 0 {
					metricName = metricName[:lastUnderscoreIndex]
				}
			}
		}
	}

	targets := ca.scrapeManager.TargetsAll()
	var metadata *scrape.MetricMetadata
	var scheme string

	jobTargets := targets[jobName]
	for _, target := range jobTargets {
		if metadata == nil {
			m, ok := target.Metadata(metricName)
			if ok {
				metadata = new(scrape.MetricMetadata)
				*metadata = m

				discoveredLabels := target.DiscoveredLabels()
				for _, discoveredLabel := range discoveredLabels {
					switch discoveredLabel.Name {
					case "__scheme__":
						scheme = discoveredLabel.Value
					}
				}
			}
		}
	}

	var err error
	if metadata == nil {
		err = errNoMetadata
	} else if metadata.Unit == "" { // Try to infer the metricName and unit heuristically.
		metricName, metadata.Unit = heuristicalMetricAndKnownUnits(metricName)
	}

	return metricName, metadata, scheme, err
}

var (
	errNoMetadata = errors.New("no metadata was parsed")
	errStaleData  = errors.New("encountered stale data since last scrape")
	errNilMetric  = errors.New("expected a non-nil Metric")
)

type metricsProcessor struct {
	mu sync.RWMutex

	definitionsByMetricName map[string][]*bucketDefinition
	lastSeenData            map[string]*lastSeenData
}

type lastSeenData struct {
	lastSeenSum          float64
	lastSeenValue        float64
	lastSeenCount        float64
	lastSeenBucketCounts []float64

	// These times below are useful
	// to aid in garbage collection.
	lastTouchTime time.Time
}

func (mp *metricsProcessor) lastSeenSum(key string) (sum float64) {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	lsbd := mp.lastSeenData[key]
	if lsbd != nil {
		sum = lsbd.lastSeenSum
	}
	return
}

func (mp *metricsProcessor) lastSeenBucketCounts(key string) (bucketCounts []float64) {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	lsbd := mp.lastSeenData[key]
	if lsbd != nil {
		bucketCounts = lsbd.lastSeenBucketCounts
	}
	return
}

func (mp *metricsProcessor) lastSeenCount(key string) (count float64) {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	lsbd := mp.lastSeenData[key]
	if lsbd != nil {
		count = lsbd.lastSeenCount
	}
	return
}

func (mp *metricsProcessor) lastSeenValue(key string) (value float64) {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	lsbd := mp.lastSeenData[key]
	if lsbd != nil {
		value = lsbd.lastSeenValue
	}
	return
}

func (mp *metricsProcessor) withNonNilAndToBeSavedBucketData(key string, useIt func(*lastSeenData)) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	lsbd, ok := mp.lastSeenData[key]
	if !ok || lsbd == nil {
		lsbd = new(lastSeenData)
	}
	useIt(lsbd)

	// Update the last touch time in order to aid
	// with eviction of least recently used items.
	lsbd.lastTouchTime = time.Now().UTC()

	mp.lastSeenData[key] = lsbd
}

func (mp *metricsProcessor) clearLastSeenBucketCounts(key string) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	lsbd, ok := mp.lastSeenData[key]
	if ok && lsbd != nil {
		lsbd.lastSeenBucketCounts = lsbd.lastSeenBucketCounts[:0]
	}
}

func (mp *metricsProcessor) setLastSeenSum(key string, newSum float64) {
	mp.withNonNilAndToBeSavedBucketData(key, func(lsbd *lastSeenData) {
		lsbd.lastSeenSum = newSum
	})
}

func (mp *metricsProcessor) setLastSeenBucketCounts(key string, newBucketCounts []float64) {
	mp.withNonNilAndToBeSavedBucketData(key, func(lsbd *lastSeenData) {
		lsbd.lastSeenBucketCounts = newBucketCounts
	})
}

func (mp *metricsProcessor) setLastSeenCount(key string, newCount float64) {
	mp.withNonNilAndToBeSavedBucketData(key, func(lsbd *lastSeenData) {
		lsbd.lastSeenCount = newCount
	})
}

func (mp *metricsProcessor) setLastSeenValue(key string, newValue float64) {
	mp.withNonNilAndToBeSavedBucketData(key, func(lsbd *lastSeenData) {
		lsbd.lastSeenValue = newValue
	})
}

func isInfBucket(def *bucketDefinition) bool {
	if def == nil {
		return false
	}
	return math.IsInf(def.bucketBoundary, -1) || math.IsInf(def.bucketBoundary, +1)
}

func (ca *customAppender) processHistogramLikeMetrics(metricName string, metadata *scrape.MetricMetadata, scheme string, labelsList labels.Labels, ref uint64, timeAtMs int64, value float64) error {
	bdef, err := ca.parseDistributions(metadata, scheme, labelsList, ref, timeAtMs, value)
	if err != nil {
		return err
	}

	key := hash(bdef.metricName, bdef.orderedTags)

	// Get the stored stack first.
	mp := ca.metricsProcessor
	stack := mp.definitionsByMetricName[key]

	// Before processing a metric, we have to have all 3 markers:
	// * bucket
	// * sum
	// * count
	var typeBitSet distributionMarker
	var hasTerminalBucketInf bool
	for _, def := range stack {
		typeBitSet |= def.marker
		if isInfBucket(def) {
			hasTerminalBucketInf = true
		}
	}

	hasAllMarkers := (typeBitSet & allMarkersSet) == allMarkersSet
	readyForProcessing := hasAllMarkers && hasTerminalBucketInf
	if !readyForProcessing {
		stack = append(stack, bdef)
		mp.definitionsByMetricName[key] = stack
		return nil
	}

	// Otherwise we are at terminal state for this bucket
	// and we can now compile the metric.
	// Firstly, unmemoize the stack of bucket definitions.
	delete(mp.definitionsByMetricName, key)

	// Filter out the bucket with Inf
	var infBucket *bucketDefinition

	var maxSum float64
	var buckets []*bucketDefinition // All the buckets with the InfBucket excluded.
	for _, definition := range stack {
		switch definition.marker {
		case markerBucket:
			if isInfBucket(definition) {
				infBucket = definition
			} else {
				buckets = append(buckets, definition)
			}

		case markerSum:
			if definition.sum > maxSum {
				maxSum = definition.sum
			}
		}
	}

	// Sort them by bucket boundaries firstly.
	sort.Slice(buckets, func(i, j int) bool {
		bdi, bdj := buckets[i], buckets[j]
		return bdi.bucketBoundary < bdj.bucketBoundary
	})

	// Now given that the bucket boundaries are sorted, the last
	// bucket is the terminal bucket i.e. Inf. Its values are the
	// remnants on the outer side of the last bucket.
	if n := len(buckets); n > 0 {
		lastNonInfiniteBucket := buckets[n-1]
		if diff := infBucket.bucketCount - lastNonInfiniteBucket.bucketCount; diff > 0 {
			lastNonInfiniteBucket.sum += infBucket.sum
			lastNonInfiniteBucket.totalCount += infBucket.totalCount
			lastNonInfiniteBucket.bucketCount += infBucket.bucketCount
			buckets[n-1] = lastNonInfiniteBucket
		}
	}

	monotonicCounts := make([]float64, len(buckets))
	rawBucketBounds := make([]float64, len(buckets))
	for i, bd := range buckets {
		monotonicCounts[i] = bd.bucketCount
		rawBucketBounds[i] = bd.bucketBoundary
	}

	// Because Prometheus cumulatively inserts counts.
	// We need to further subtract counts from the counts
	// that were performed during the last scrape.
	// This is more of a heuristic because if sources are taken
	// down then resurrected, in the midst of a scrape, that will
	// yield inaccurate results.
	lastSeenBucketCounts := mp.lastSeenBucketCounts(key)
	lastSeenSum := mp.lastSeenSum(key)

	if len(lastSeenBucketCounts) != len(monotonicCounts) { // Perhaps the first time.
		mp.setLastSeenBucketCounts(key, monotonicCounts[:])
		mp.setLastSeenSum(key, maxSum)
	} else { // This is a probable match for values we've probably already seen.
		dedupedBucketCounts := make([]float64, len(monotonicCounts))
		// If a scrape target was restarted, its "cumulative" bucket counts
		// will all start afresh. However, since Prometheus buckets are cumulative,
		// doing: now[i] - prev[i] should always result in >=0
		// thus any deduped value that's <0 indicates that we should discard
		// any deduplication processes and that this is the first of its kind
		// data being scraped.
		canDedupe := true
		for i, nowi := range monotonicCounts {
			previ := lastSeenBucketCounts[i]
			deduped := nowi - previ
			if deduped < 0 { // No need to proceed with deduplication.
				canDedupe = false
				break
			}

			// Otherwise good to go on with deduplication.
			dedupedBucketCounts[i] = deduped
			// In place replace the old counts with the new ones.
			lastSeenBucketCounts[i] = nowi
		}

		if !canDedupe {
			// This data is the first of its kind and could most
			// probably indicate that a scrape target was taken down
			// or all its data cleared hence it started afresh.
			mp.clearLastSeenBucketCounts(key)
			return nil
		}

		// Otherwise now memoize the deduped bucket counts
		// for memoization in the next cycle of scraping.
		mp.setLastSeenBucketCounts(key, lastSeenBucketCounts[:])
		// Also memoize the last seen sum.
		mp.setLastSeenSum(key, maxSum)
		monotonicCounts = dedupedBucketCounts
	}

	// Since Prometheus values are monotonically accumulated, let's isolate discrete counts.
	discreteCounts := make([]float64, len(monotonicCounts))
	var lastSeenCount float64
	var totalCount float64
	var maxPlausibleSum float64
	for i, monotonicCount := range monotonicCounts {
		// Buckets are monotonic i.e. either all increasing or all decreasing.
		// Hence, we need to subtract the current count from the last seen count.
		discreteCount := monotonicCount - lastSeenCount
		discreteCounts[i] = discreteCount
		totalCount += discreteCount
		maxPlausibleSum += (discreteCount * rawBucketBounds[i])
		lastSeenCount = monotonicCount
	}

	instantenousSum := maxSum - lastSeenSum
	// fmt.Printf("DiscreteCounts:       %+v\n\nLastSeenSum: %.3f\nMaxSum:      %.3f\nDiff:        %.3f\033[00m\n\n",
	// 	discreteCounts, lastSeenSum, maxSum, instantenousSum)
	// Sanity checks, if the count is unrelatable
	// to the bucket counts we'll reject this data.
	if instantenousSum > maxPlausibleSum {
		// This data is invalid. It is impossible for the instantenous
		// sum to be greater than the maximum plausible sum.
		// fmt.Printf("\033[31mInvalidating %+v because\n*instantaneousSum(%.3f) > maxPlausibleSum(%.3f)\033[00m\n\n",
		// 	discreteCounts, instantenousSum, maxPlausibleSum)
		return nil
	} else if instantenousSum <= 0.0 && maxPlausibleSum > 0 {
		// Clearly there is a miscalculation with the last scraped sum
		// because instantenousSum can't be <= 0.0 yet here we have
		// at least 1 element.
	}

	var mean float64
	if totalCount > 0 {
		mean = instantenousSum / totalCount
	} else {
		instantenousSum = 0.0
	}

	var sumOfSquaredDeviation float64
	protoBuckets := make([]*metricspb.DistributionValue_Bucket, len(discreteCounts))
	for i := 0; i < len(discreteCounts); i++ {
		discreteCount := discreteCounts[i]
		sumOfSquaredDeviation += math.Pow(discreteCount-mean, 2)
		protoBuckets[i] = &metricspb.DistributionValue_Bucket{
			Count: int64(discreteCount),
		}
	}

	// TODO(@odeke-em): For consistency, ensure that all the discrete
	// parts of a distribution share the exact same timestamp.
	startTimestamp := timestampFromMs(infBucket.timeAtMs)

	dv := &metricspb.DistributionValue{
		Buckets:               protoBuckets,
		Count:                 int64(totalCount),
		Sum:                   instantenousSum,
		SumOfSquaredDeviation: sumOfSquaredDeviation,
		BucketOptions: &metricspb.DistributionValue_BucketOptions{
			Type: &metricspb.DistributionValue_BucketOptions_Explicit_{
				Explicit: &metricspb.DistributionValue_BucketOptions_Explicit{
					Bounds: rawBucketBounds,
				},
			},
		},
	}

	labelKeys, labelValues := orderedTagsToLabelKeysLabelValues(infBucket.orderedTags)
	point := &metricspb.Point{
		Timestamp: startTimestamp,
		Value: &metricspb.Point_DistributionValue{
			DistributionValue: dv,
		},
	}

	md := &metricspb.MetricDescriptor{
		Name:        infBucket.metricName,
		Description: infBucket.description,
		Unit:        infBucket.unit,
		Type:        infBucket.typ,
		LabelKeys:   labelKeys,
	}

	node := nodeFromJobNameAddress(infBucket.nodeName, infBucket.address, infBucket.scheme)

	bdl := &bundling{
		md:          md,
		node:        node,
		resource:    nil,
		point:       point,
		ts:          startTimestamp,
		labelValues: labelValues,
	}

	ca.allMetricsBundler.Add(bdl, 1)

	return nil
}

type distributionMarker int

const (
	markerCount distributionMarker = 1 + (iota << 1)
	markerSum
	markerBucket
)

const allMarkersSet = markerCount | markerSum | markerBucket

type bucketDefinition struct {
	sum        float64
	typ        metricspb.MetricDescriptor_Type
	unit       string
	metricName string
	nodeName   string
	scheme     string
	address    string

	timeAtMs       int64
	totalCount     float64
	bucketBoundary float64
	bucketCount    float64
	description    string
	marker         distributionMarker
	orderedTags    labels.Labels
}

var errNotHistogramLike = errors.New("not histogram-like")

func (ca *customAppender) parseDistributions(metadata *scrape.MetricMetadata, scheme string, labelsList labels.Labels, ref uint64, timeAtMs int64, value float64) (*bucketDefinition, error) {
	var address, metricName, jobName, distributionQualifier string
	var bucketBoundary float64
	var sum, totalCount float64
	var orderedTags labels.Labels

	for _, label := range labelsList {
		switch label.Name {
		case labels.MetricName:
			metricName = label.Value
			// Don't waste time processing Prometheus scrape metrics.
			if metricName == "up" || strings.HasPrefix(metricName, "scrape_") {
				return nil, fmt.Errorf("%q is not an interesting metric", metricName)
			}

			switch {
			case strings.HasSuffix(metricName, "_count"):
				totalCount = value
				metricName = strings.TrimSuffix(metricName, "_count")
				distributionQualifier = "count"

			case strings.HasSuffix(metricName, "_sum"):
				distributionQualifier = "sum"
				metricName = strings.TrimSuffix(metricName, "_sum")
				sum = value

			case strings.HasSuffix(metricName, "_bucket"):
				distributionQualifier = "bucket"

			default:
				orderedTags = append(orderedTags, label)
			}

		case labels.BucketLabel:
			bucketBoundary, _ = strconv.ParseFloat(label.Value, 64)
			if len(metricName) > 0 {
				lastUnderscoreIndex := strings.LastIndex(metricName, "_")
				if lastUnderscoreIndex > 0 {
					distributionQualifier = metricName[lastUnderscoreIndex+1:]
					metricName = metricName[:lastUnderscoreIndex]
				}
			}

		case "job":
			// Do nothing
			jobName = label.Value

		case labels.InstanceName:
			address = label.Value

		default:
			orderedTags = append(orderedTags, label)
		}
	}

	if metadata == nil {
		// By this point, we don't have the metadata necessary to
		// construct an OpenCensus Metric from this scraped data.
		// Perhaps this could be a result from this scraper monitoring itself.
		return nil, errNoMetadata
	}

	if metadata.Unit == "" { // Try to infer the metricName and unit heuristically.
		metricName, metadata.Unit = heuristicalMetricAndKnownUnits(metricName)
	}

	definition := &bucketDefinition{
		address:     address,
		description: metadata.Help,
		scheme:      scheme,
		nodeName:    jobName,
		orderedTags: orderedTags,
		timeAtMs:    timeAtMs,
		unit:        metadata.Unit,
	}

	switch metadataType := string(metadata.Type); strings.ToLower(metadataType) {
	case "gaugehistogram", "histogram":
		typ := metricspb.MetricDescriptor_GAUGE_DISTRIBUTION
		if metadataType == "histogram" {
			typ = metricspb.MetricDescriptor_CUMULATIVE_DISTRIBUTION
		}
		definition.typ = typ

		switch distributionQualifier {
		case "sum":
			definition.sum = sum
			definition.marker = markerSum

		case "count":
			definition.totalCount = totalCount
			definition.marker = markerCount

		case "bucket":
			definition.bucketBoundary = bucketBoundary
			definition.bucketCount = value
			definition.marker = markerBucket
		}

		definition.metricName = strings.TrimSuffix(metricName, "_"+distributionQualifier)

	default:
		return nil, errNotHistogramLike
	}

	return definition, nil
}

func (ca *customAppender) processNonHistogramLikeMetrics(metricName string, metadata *scrape.MetricMetadata, scheme string, labelsList labels.Labels, ref uint64, timeAtMs int64, value float64) error {
	var address, jobName string
	var orderedTags labels.Labels

	for _, label := range labelsList {
		switch label.Name {
		case "job":
			jobName = label.Value

		case labels.BucketLabel:
			// Should not happen since buckets are processed in a
			// different route but nonetheless, do nothing here.

		case labels.InstanceName:
			address = label.Value

		case labels.AlertName:
			// Do nothing.

		case labels.MetricName:
			// Do nothing.

		default:
			orderedTags = append(orderedTags, label)
		}
	}

	if metadata == nil {
		// By this point, we don't have the metadata necessary to
		// construct an OpenCensus Metric from this scraped data.
		// Perhaps this could be a result from this scraper monitoring itself.
		return errNoMetadata
	}

	if metadata.Unit == "" { // Try to infer the metricName and unit heuristically.
		metricName, metadata.Unit = heuristicalMetricAndKnownUnits(metricName)
	}

	// And now it is conversion time.

	atTimestamp := timestampFromMs(timeAtMs)
	point := &metricspb.Point{
		Timestamp: atTimestamp,
	}

	labelKeys, labelValues := orderedTagsToLabelKeysLabelValues(orderedTags)
	var metricDescriptor *metricspb.MetricDescriptor

	switch metadataType := string(metadata.Type); strings.ToLower(metadataType) {
	case "counter":
		mp := ca.metricsProcessor
		key := hash(metricName, orderedTags)
		lastSeenCount := mp.lastSeenCount(key)
		// Memoize the current value as the last seen count.
		mp.setLastSeenCount(key, value)

		// Now compute the discrete count since
		// Prometheus data is cumulatively stored.
		discreteCount := value - lastSeenCount
		if discreteCount < 0 {
			// This is stale data that we are dealing with.
			// We need to discard it.
			return errStaleData
		}

		metricDescriptor = &metricspb.MetricDescriptor{
			Name:        metricName,
			Unit:        metadata.Unit,
			Description: metadata.Help,
			Type:        metricspb.MetricDescriptor_CUMULATIVE_INT64,
			LabelKeys:   labelKeys,
		}
		point.Value = &metricspb.Point_Int64Value{
			Int64Value: int64(math.Round(discreteCount)),
		}

	case "gauge":
		mp := ca.metricsProcessor
		key := hash(metricName, orderedTags)
		lastSeenValue := mp.lastSeenValue(key)
		// Memoize the current value as the last seen count.
		mp.setLastSeenValue(key, value)

		// Now compute the discrete value since
		// Prometheus data is cumulatively stored.
		discreteValue := value - lastSeenValue
		if discreteValue < 0 {
			// This is stale data that we are dealing with.
			// We need to discard it.
			return errStaleData
		}

		metricDescriptor = &metricspb.MetricDescriptor{
			Name:        metricName,
			Unit:        metadata.Unit,
			Description: metadata.Help,
			Type:        metricspb.MetricDescriptor_GAUGE_DOUBLE,
			LabelKeys:   labelKeys,
		}
		point.Value = &metricspb.Point_DoubleValue{
			DoubleValue: discreteValue,
		}

	default:
		return fmt.Errorf("unknown recognized metric type: %s", metadataType)
	}

	node := nodeFromJobNameAddress(jobName, address, scheme)
	bdl := &bundling{
		md:          metricDescriptor,
		node:        node,
		resource:    nil,
		point:       point,
		ts:          atTimestamp,
		labelValues: labelValues,
	}

	ca.allMetricsBundler.Add(bdl, 1)
	return nil
}

func nodeFromJobNameAddress(jobName, address, scheme string) *commonpb.Node {
	if jobName == "" && address == "" {
		return nil
	}
	var serviceInfo *commonpb.ServiceInfo
	if jobName != "" {
		serviceInfo = &commonpb.ServiceInfo{Name: jobName}
	}
	var processIdentifier *commonpb.ProcessIdentifier
	var host, port string
	if address != "" {
		host, port = hostPort(address)
		processIdentifier = &commonpb.ProcessIdentifier{
			HostName: host,
		}
	}
	node := &commonpb.Node{
		ServiceInfo: serviceInfo,
		Identifier:  processIdentifier,
		Attributes: map[string]string{
			"scheme": scheme,
			"port":   port,
		},
	}
	return node
}

func heuristicalMetricAndKnownUnits(metricName string) (finalMetricName, unit string) {
	lastUnderscoreIndex := strings.LastIndex(metricName, "_")
	if lastUnderscoreIndex <= 0 || lastUnderscoreIndex >= len(metricName)-1 {
		return metricName, ""
	}

	supposedUnit := metricName[lastUnderscoreIndex+1:]
	switch strings.ToLower(supposedUnit) {
	case "millisecond", "milliseconds", "ms":
		unit = "ms"
	case "second", "seconds", "s":
		unit = "s"
	case "microsecond", "microseconds", "us":
		unit = "us"
	case "nanosecond", "nanoseconds", "ns":
		unit = "ns"
	case "byte", "bytes", "by":
		unit = "By"
	case "bit", "bits":
		unit = "By"
	case "kilogram", "kilograms", "kg":
		unit = "kg"
	case "gram", "grams", "g":
		unit = "g"
	case "meter", "meters", "metre", "metres", "m":
		unit = "m"
	case "kilometer", "kilometers", "kilometre", "kilometres", "km":
		unit = "km"
	case "milimeter", "milimeters", "milimetre", "milimetres", "mm":
		unit = "mm"
	case "nanogram", "ng", "nanograms":
		unit = "ng"
	}

	if unit != "" { // A heuristic matched a probable unit, so perform the split.
		finalMetricName = metricName[:lastUnderscoreIndex]
	} else {
		finalMetricName = metricName
	}

	return finalMetricName, unit
}

func hostPort(addr string) (host, port string) {
	if index := strings.LastIndex(addr, ":"); index < 0 {
		host = addr
	} else {
		host, port = addr[:index], addr[index+1:]
	}
	return
}

func hash(metricName string, orderedTags labels.Labels) string {
	return fmt.Sprintf("%s-%d", metricName, orderedTags.Hash())
}

func orderedTagsToLabelKeysLabelValues(orderedTags labels.Labels) (labelKeys []*metricspb.LabelKey, labelValues []*metricspb.LabelValue) {
	if len(orderedTags) == 0 {
		return
	}

	labelKeys = make([]*metricspb.LabelKey, len(orderedTags))
	labelValues = make([]*metricspb.LabelValue, len(orderedTags))
	for i, tag := range orderedTags {
		labelKeys[i] = &metricspb.LabelKey{Key: tag.Name}
		labelValues[i] = &metricspb.LabelValue{Value: tag.Value}
	}

	return
}

func timestampFromMs(timeAtMs int64) *timestamp.Timestamp {
	secs, ns := timeAtMs/1e3, (timeAtMs%1e3)*1e6
	return &timestamp.Timestamp{
		Seconds: secs,
		Nanos:   int32(ns),
	}
}

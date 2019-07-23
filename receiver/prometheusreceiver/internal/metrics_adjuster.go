package internal

import (
	"fmt"
	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
	"github.com/golang/protobuf/ptypes/wrappers"
	"go.uber.org/zap"
	"strings"
	"sync"
	"time"
)

// Notes on garbage collection (gc):
//
// Job-level gc:
// The Prometheus receiver will likely execute in a long running service whose lifetime may exceed
// the lifetimes of many of the jobs that it is collecting from. In order to keep the JobsMap from
// getting overwhelmed by entries of no-longer existing jobs, the JobsMap has a timestamp tracking
// the last time is was gc'd and each timeseriesMap entry in the JobsMap has a timestamp tracking
// the last time it was accessed.
//
// At the end of each JobsMap.get(), if the last time the JobsMap was gc'd exceeds the 'gcInterval',
// the JobsMap is locked and any entries whose last access exceeds the 'jobMaxLifetime' is removed
// from the JobMap. This collection is done in a go routine to prevent blocking the return of
// the original call to JobsMap.get().
//
// Timeseries-level gc:
// Some jobs that the Prometheus receiver is collecting from may export timeseries based on metrics
// from other jobs (e.g. cAdvisor). In order to keep the timeseriesMap from getting overwhelmed by
// timeseriesinfo of no-longer existing jobs, each timeseriesinfo entry in the map has a timestamp
// tracking the last time it was accessed.
//
// At the end of each call to MetricAdjuster.AdjustMetrics(), when the associated timeseriesMap is
// already locked, each timeseriesinfo entry is examined and removed from the timeseriesMap if
// its last access exceeds 'timeseriesMaxLife'.
//
// Alternative Strategies:
// 1. The overhead of timeseries-level gc is expected to be low but, if it is not, the gc can be
//    done at the same time that the job-level gc is done. This approach adds overhead to the
//    job-level gc and, potentially, more contention so the simpler approach is currently used.
//
// 2. If the job-level gc doesn't run often enough, or runs too often, a separate go routine can
//    be spawned at JobMap creation time that gc's at periodic intervals. This approach potentially
//    adds more contention and latency to each scrape so the current approach is used. Note that
//    the go routine will need to be cancelled upon StopMetricsReception().

contains the information necessary to adjust from the initial point and to
// detect resets.
type timeseriesinfo struct {
	lastAccess time.Time
	initial    *metricspb.TimeSeries
	previous   *metricspb.TimeSeries
}

// timeseriesMap maps from a timeseries instance (metric * label values) to the timeseries info for
// the instance.
type timeseriesMap struct {
	sync.RWMutex
	timeseriesMaxLifetime time.Duration
	lastAccess            time.Time
	tsiMap                map[string]*timeseriesinfo
}

// Get the timeseriesinfo for the timeseries associated with the metric and label values.
func (tsm *timeseriesMap) get(
	metric *metricspb.Metric, values []*metricspb.LabelValue) *timeseriesinfo {
	name := metric.GetMetricDescriptor().GetName()
	sig := getTimeseriesSignature(name, values)
	tsi, ok := tsm.tsiMap[sig]
	if !ok {
		tsi = &timeseriesinfo{}
		tsm.tsiMap[sig] = tsi
	}
	tsi.lastAccess = tsm.lastAccess
	return tsi
}

// Remove timeseries that have aged out.
func (tsm *timeseriesMap) gc() {
	current := time.Now()
	for ts, tsi := range tsm.tsiMap {
		if current.Sub(tsi.lastAccess) > tsm.timeseriesMaxLifetime {
			delete(tsm.tsiMap, ts)
		}
	}
}

func newTimeseriesMap(timeseriesMaxLifetime time.Duration) *timeseriesMap {
	return &timeseriesMap{
		timeseriesMaxLifetime: timeseriesMaxLifetime,
		lastAccess:            time.Now(),
		tsiMap:                map[string]*timeseriesinfo{},
	}
}

// Create a unique timeseries signature consisting of the metric name and label values.
func getTimeseriesSignature(name string, values []*metricspb.LabelValue) string {
	labelValues := make([]string, 0, len(values))
	for _, label := range values {
		if label.GetValue() != "" {
			labelValues = append(labelValues, label.GetValue())
		}
	}
	return fmt.Sprintf("%s,%s", name, strings.Join(labelValues, ","))
}

// JobsMap maps from a job instance to a map of timeseries instances for the job.
type JobsMap struct {
	sync.RWMutex
	gcInterval            time.Duration
	jobMaxLifetime        time.Duration
	timeseriesMaxLifetime time.Duration
	lastGC                time.Time
	jobsMap               map[string]*timeseriesMap
}

const (
	defaultGcInterval            = time.Duration(5 * time.Minute)
	defaultJobMaxLifetime        = time.Duration(2 * time.Minute)
	defaultTimeseriesMaxLifetime = time.Duration(2 * time.Minute)
)

// NewJobsMap creates a new (empty) JobsMap.
func NewJobsMap() *JobsMap {
	return newJobsMapWithDurations(defaultGcInterval, defaultJobMaxLifetime,
		defaultTimeseriesMaxLifetime)
}

// newJobsMap creates a new (empty) JobsMap with specified durations.
func newJobsMapWithDurations(gcInterval, jobMaxLifetime, timeseriesMaxLifetime time.Duration) *JobsMap {
	return &JobsMap{
		gcInterval:            gcInterval,
		jobMaxLifetime:        jobMaxLifetime,
		timeseriesMaxLifetime: timeseriesMaxLifetime,
		lastGC:                time.Now(),
		jobsMap:               make(map[string]*timeseriesMap),
	}
}

func (jm *JobsMap) gc(gcInterval time.Duration, lastGC time.Time) {
	current := time.Now()
	if current.Sub(lastGC) > gcInterval {
		go func() {
			jm.Lock()
			if current.Sub(jm.lastGC) > jm.gcInterval {
				for sig, tsm := range jm.jobsMap {
					tsm.Lock()
					if current.Sub(tsm.lastAccess) > jm.jobMaxLifetime {
						delete(jm.jobsMap, sig)
					}
					tsm.Unlock()
				}
				jm.lastGC = time.Now()
			}
			jm.Unlock()
		}()
	}
}

func (jm *JobsMap) get(job, instance string) *timeseriesMap {
	sig := job + ":" + instance
	jm.RLock()
	jobMaxLifetime := jm.jobMaxLifetime
	lastGC := jm.lastGC
	tsm, ok := jm.jobsMap[sig]
	jm.RUnlock()
	defer jm.gc(jobMaxLifetime, lastGC)
	if ok {
		return tsm
	}
	jm.Lock()
	defer jm.Unlock()
	tsm2, ok2 := jm.jobsMap[sig]
	if ok2 {
		return tsm2
	}
	tsm2 = newTimeseriesMap(jm.timeseriesMaxLifetime)
	jm.jobsMap[sig] = tsm2
	return tsm2
}

// MetricsAdjuster takes a map from a metric instance to the initial point in the metrics instance
// and provides AdjustMetrics, which takes a sequence of metrics and adjust their values based on
// the initial points.
type MetricsAdjuster struct {
	tsm    *timeseriesMap
	logger *zap.SugaredLogger
}

// NewMetricsAdjuster is a constructor for MetricsAdjuster.
func NewMetricsAdjuster(tsm *timeseriesMap, logger *zap.SugaredLogger) *MetricsAdjuster {
	return &MetricsAdjuster{
		tsm:    tsm,
		logger: logger,
	}
}

// AdjustMetrics takes a sequence of metrics and adjust their values based on the initial and
// previous points in the timeseriesMap. If the metric is the first point in the timeseries, or the
// timeseries has been reset, it is removed from the sequence and added to the the timeseriesMap.
func (ma *MetricsAdjuster) AdjustMetrics(metrics []*metricspb.Metric) []*metricspb.Metric {
	var adjusted = make([]*metricspb.Metric, 0, len(metrics))
	ma.tsm.Lock()
	ma.tsm.lastAccess = time.Now()
	for _, metric := range metrics {
		if ma.adjustMetric(metric) {
			adjusted = append(adjusted, metric)
		}
	}
	ma.tsm.gc()
	ma.tsm.Unlock()
	return adjusted
}

// Returns true if at least one of the metric's timeseries was adjusted and false if all of the
// timeseries are an initial occurence or a reset.
//
// Types of metrics returned supported by prometheus:
// - MetricDescriptor_GAUGE_DOUBLE
// - MetricDescriptor_GAUGE_DISTRIBUTION
// - MetricDescriptor_CUMULATIVE_DOUBLE
// - MetricDescriptor_CUMULATIVE_DISTRIBUTION
// - MetricDescriptor_SUMMARY
func (ma *MetricsAdjuster) adjustMetric(metric *metricspb.Metric) bool {
	switch metric.MetricDescriptor.Type {
	case metricspb.MetricDescriptor_GAUGE_DOUBLE, metricspb.MetricDescriptor_GAUGE_DISTRIBUTION:
		// gauges don't need to be adjusted so no additional processing is necessary
		return true
	default:
		return ma.adjustMetricTimeseries(metric)
	}
}

// Returns true if at least one of the metric's timeseries was adjusted and false if all of the
// timeseries are an initial occurence or a reset.
func (ma *MetricsAdjuster) adjustMetricTimeseries(metric *metricspb.Metric) bool {
	filtered := make([]*metricspb.TimeSeries, 0, len(metric.GetTimeseries()))
	for _, current := range metric.GetTimeseries() {
		tsi := ma.tsm.get(metric, current.GetLabelValues())
		if tsi.initial == nil {
			// initial timeseries
			tsi.initial = current
			tsi.previous = current
		} else {
			if ma.adjustTimeseries(metric.MetricDescriptor.Type, current, tsi.initial,
				tsi.previous) {
				tsi.previous = current
				filtered = append(filtered, current)
			} else {
				// reset timeseries
				tsi.initial = current
				tsi.previous = current
			}
		}
	}
	metric.Timeseries = filtered
	return len(filtered) > 0
}

// Returns true if 'current' was adjusted and false if 'current' is an the initial occurence or a
// reset of the timeseries.
func (ma *MetricsAdjuster) adjustTimeseries(metricType metricspb.MetricDescriptor_Type,
	current, initial, previous *metricspb.TimeSeries) bool {
	if !ma.adjustPoints(
		metricType, current.GetPoints(), initial.GetPoints(), previous.GetPoints()) {
		return false
	}
	current.StartTimestamp = initial.StartTimestamp
	return true
}

func (ma *MetricsAdjuster) adjustPoints(metricType metricspb.MetricDescriptor_Type,
	current, initial, previous []*metricspb.Point) bool {
	if len(current) != 1 || len(initial) != 1 || len(current) != 1 {
		ma.logger.Infof(
			"len(current): %v, len(initial): %v, len(previous): %v should all be 1",
			len(current), len(initial), len(previous))
		return true
	}
	return ma.adjustPoint(metricType, current[0], initial[0], previous[0])
}

// Note: There is an important, subtle point here. When a new timeseries or a reset is detected,
// current and initial are the same object. When initial == previous, the previous value/count/sum
// are all the initial value. When initial != previous, the previous value/count/sum has been
// adjusted wrt the initial value so both they must be combined to find the actual previous
// value/count/sum. This happens because the timeseries are updated in-place - if new copies of the
// timeseries were created instead, previous could be used directly but this would mean reallocating
// all of the metrics.
func (ma *MetricsAdjuster) adjustPoint(metricType metricspb.MetricDescriptor_Type,
	current, initial, previous *metricspb.Point) bool {
	switch metricType {
	case metricspb.MetricDescriptor_CUMULATIVE_DOUBLE:
		currentValue := current.GetDoubleValue()
		initialValue := initial.GetDoubleValue()
		previousValue := initialValue
		if initial != previous {
			previousValue += previous.GetDoubleValue()
		}
		if currentValue < previousValue {
			// reset detected
			return false
		}
		current.Value =
			&metricspb.Point_DoubleValue{DoubleValue: currentValue - initialValue}
	case metricspb.MetricDescriptor_CUMULATIVE_DISTRIBUTION:
		// note: sum of squared deviation not currently supported
		currentDist := current.GetDistributionValue()
		initialDist := initial.GetDistributionValue()
		previousCount := initialDist.Count
		previousSum := initialDist.Sum
		if initial != previous {
			previousCount += previous.GetDistributionValue().Count
			previousSum += previous.GetDistributionValue().Sum
		}
		if currentDist.Count < previousCount || currentDist.Sum < previousSum {
			// reset detected
			return false
		}
		currentDist.Count -= initialDist.Count
		currentDist.Sum -= initialDist.Sum
		ma.adjustBuckets(currentDist.Buckets, initialDist.Buckets)
	case metricspb.MetricDescriptor_SUMMARY:
		// note: for summary, we don't adjust the snapshot
		currentCount := current.GetSummaryValue().Count.GetValue()
		currentSum := current.GetSummaryValue().Sum.GetValue()
		initialCount := initial.GetSummaryValue().Count.GetValue()
		initialSum := initial.GetSummaryValue().Sum.GetValue()
		previousCount := initialCount
		previousSum := initialSum
		if initial != previous {
			previousCount += previous.GetSummaryValue().Count.GetValue()
			previousSum += previous.GetSummaryValue().Sum.GetValue()
		}
		if currentCount < previousCount || currentSum < previousSum {
			// reset detected
			return false
		}
		current.GetSummaryValue().Count =
			&wrappers.Int64Value{Value: currentCount - initialCount}
		current.GetSummaryValue().Sum =
			&wrappers.DoubleValue{Value: currentSum - initialSum}
	default:
		// this shouldn't happen
		ma.logger.Infof("adjust unexpect point type %v, skipping ...", metricType.String())
	}
	return true
}

func (ma *MetricsAdjuster) adjustBuckets(current, initial []*metricspb.DistributionValue_Bucket) {
	if len(current) != len(initial) {
		// this shouldn't happen
		ma.logger.Infof("len(current buckets): %v != len(initial buckets): %v",
			len(current), len(initial))
	}
	for i := 0; i < len(current); i++ {
		current[i].Count -= initial[i].Count
	}
}

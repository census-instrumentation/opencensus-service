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

package internal

import (
	"errors"
	"fmt"
	"go.uber.org/zap"
	"strconv"
	"strings"

	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/textparse"
)

const metricsSuffixCount = "_count"
const metricsSuffixBucket = "_bucket"
const metricsSuffixSum = "_sum"

var trimmableSuffixes = []string{metricsSuffixBucket, metricsSuffixCount, metricsSuffixSum}
var errNoDataToBuild = errors.New("there's no data to build")
var errNoBoundaryLabel = errors.New("given metricType has no BucketLabel or QuantileLabel")
var errEmptyBoundaryLabel = errors.New("BucketLabel or QuantileLabel is empty")

var dummyMetrics = make([]*metricspb.Metric, 0)

type metricBuilder struct {
	ts                int64
	hasData           bool
	hasInternalMetric bool
	mc                MetadataCache
	metrics           []*metricspb.Metric
	logger            *zap.SugaredLogger
	currentMf         MetricFamily
}

// newMetricBuilder creates a MetricBuilder which is allowed to feed all the datapoints from a single prometheus
// scraped page by calling its AddDataPoint function, and turn them into an opencensus data.MetricsData object
// by calling its Build function
func newMetricBuilder(mc MetadataCache, logger *zap.SugaredLogger) *metricBuilder {

	return &metricBuilder{
		mc:      mc,
		metrics: make([]*metricspb.Metric, 0),
		logger:  logger,
	}
}

// AddDataPoint is for feeding prometheus data complexValue in its processing order
func (b *metricBuilder) AddDataPoint(ls labels.Labels, t int64, v float64) error {
	metricName := ls.Get(model.MetricNameLabel)
	if metricName == "" {
		return errMetricNameNotFound
	} else if shouldSkip(metricName) {
		b.hasInternalMetric = true
		lm := ls.Map()
		delete(lm, model.MetricNameLabel)
		b.logger.Infow("skip internal metric", "name", metricName, "ts", t, "value", v, "labels", lm)
		return nil
	}

	b.hasData = true

	if b.currentMf != nil && !b.currentMf.IsSameFamily(metricName) {
		m := b.currentMf.ToMetric()
		if m != nil {
			b.metrics = append(b.metrics, m)
		}
		b.currentMf = newMetricFamily(metricName, b.mc)
	} else if b.currentMf == nil {
		b.currentMf = newMetricFamily(metricName, b.mc)
	}

	return b.currentMf.Add(metricName, ls, t, v)
}

// Build is to build an opencensus data.MetricsData based on all added data complexValue
func (b *metricBuilder) Build() ([]*metricspb.Metric, error) {
	if !b.hasData {
		if b.hasInternalMetric {
			return dummyMetrics, nil
		}
		return nil, errNoDataToBuild
	}

	if b.currentMf != nil {
		if m := b.currentMf.ToMetric(); m != nil {
			b.metrics = append(b.metrics, m)
		}
		b.currentMf = nil
	}

	return b.metrics, nil
}

// TODO: move the following helper functions to a proper place, as they are not called directly in this go file

func isUsefulLabel(mType metricspb.MetricDescriptor_Type, labelKey string) bool {
	result := false
	switch labelKey {
	case model.MetricNameLabel:
	case model.InstanceLabel:
	case model.SchemeLabel:
	case model.MetricsPathLabel:
	case model.JobLabel:
	case model.BucketLabel:
		result = mType != metricspb.MetricDescriptor_GAUGE_DISTRIBUTION &&
			mType != metricspb.MetricDescriptor_CUMULATIVE_DISTRIBUTION
	case model.QuantileLabel:
		result = mType != metricspb.MetricDescriptor_SUMMARY
	default:
		result = true
	}
	return result
}

// dpgSignature is used to create a key for data complexValue belong to a same group of a metric family
func dpgSignature(orderedKnownLabelKeys []string, ls labels.Labels) string {
	sign := make([]string, 0, len(orderedKnownLabelKeys))
	for _, k := range orderedKnownLabelKeys {
		v := ls.Get(k)
		if v == "" {
			continue
		}
		sign = append(sign, k+"="+v)
	}
	return fmt.Sprintf("%#v", sign)
}

func normalizeMetricName(name string) string {
	for _, s := range trimmableSuffixes {
		if strings.HasSuffix(name, s) && name != s {
			return strings.TrimSuffix(name, s)
		}
	}
	return name
}

func getBoundary(metricType metricspb.MetricDescriptor_Type, labels labels.Labels) (float64, error) {
	labelName := ""
	if metricType == metricspb.MetricDescriptor_CUMULATIVE_DISTRIBUTION ||
		metricType == metricspb.MetricDescriptor_GAUGE_DISTRIBUTION {
		labelName = model.BucketLabel
	} else if metricType == metricspb.MetricDescriptor_SUMMARY {
		labelName = model.QuantileLabel
	} else {
		return 0, errNoBoundaryLabel
	}

	v := labels.Get(labelName)
	if v == "" {
		return 0, errEmptyBoundaryLabel
	}

	return strconv.ParseFloat(v, 64)
}

func convToOCAMetricType(metricType textparse.MetricType) metricspb.MetricDescriptor_Type {
	switch metricType {
	case textparse.MetricTypeCounter:
		// always use float64, as it's the internal data type used in prometheus
		return metricspb.MetricDescriptor_CUMULATIVE_DOUBLE
	case textparse.MetricTypeGauge:
		return metricspb.MetricDescriptor_GAUGE_DOUBLE
	case textparse.MetricTypeHistogram:
		return metricspb.MetricDescriptor_CUMULATIVE_DISTRIBUTION
	// dropping support for gaugehistogram for now until we have an official spec of its implementation
	// a draft can be found in: https://docs.google.com/document/d/1KwV0mAXwwbvvifBvDKH_LU1YjyXE_wxCkHNoCGq1GX0/edit#heading=h.1cvzqd4ksd23
	//case textparse.MetricTypeGaugeHistogram:
	//	return metricspb.MetricDescriptor_GAUGE_DISTRIBUTION
	case textparse.MetricTypeSummary:
		return metricspb.MetricDescriptor_SUMMARY
	default:
		// including: textparse.MetricTypeUnknown, textparse.MetricTypeInfo, textparse.MetricTypeStateset
		return metricspb.MetricDescriptor_UNSPECIFIED
	}
}

/*
   code borrowed from the original promreceiver
*/

func heuristicalMetricAndKnownUnits(metricName, parsedUnit string) string {
	if parsedUnit != "" {
		return parsedUnit
	}
	lastUnderscoreIndex := strings.LastIndex(metricName, "_")
	if lastUnderscoreIndex <= 0 || lastUnderscoreIndex >= len(metricName)-1 {
		return ""
	}

	unit := ""

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
		unit = "Bi"
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

	return unit
}

func timestampFromMs(timeAtMs int64) *timestamp.Timestamp {
	secs, ns := timeAtMs/1e3, (timeAtMs%1e3)*1e6
	return &timestamp.Timestamp{
		Seconds: secs,
		Nanos:   int32(ns),
	}
}

func shouldSkip(metricName string) bool {
	if metricName == "up" || strings.HasPrefix(metricName, "scrape_") {
		return true
	}
	return false
}

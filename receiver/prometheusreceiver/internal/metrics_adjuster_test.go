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
	"reflect"
	"testing"

	"go.uber.org/zap"

	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/golang/protobuf/ptypes/wrappers"

	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
)

func Test_gaugeDouble(t *testing.T) {
	script := []*metricsAdjusterTest{{
		"Gauge: first timeseries - gauge is never adjusted",
		[]*metricspb.Metric{gauge(keys0, timeseries(100, vals0, double(100, 44.0)))},
		[]*metricspb.Metric{gauge(keys0, timeseries(100, vals0, double(100, 44.0)))},
	}, {
		"Gauge: second timeseries - gauge is never adjusted",
		[]*metricspb.Metric{gauge(keys0, timeseries(150, vals0, double(150, 66.0)))},
		[]*metricspb.Metric{gauge(keys0, timeseries(150, vals0, double(150, 66.0)))},
	}, {
		"Gauge: third timeseries - value less than previous value - gauge is never adjusted",
		[]*metricspb.Metric{gauge(keys0, timeseries(200, vals0, double(200, 55.0)))},
		[]*metricspb.Metric{gauge(keys0, timeseries(200, vals0, double(200, 55.0)))},
	}}
	runScript(t, script)
}

func Test_gaugeDistribution(t *testing.T) {
	script := []*metricsAdjusterTest{{
		"GaugeDist: first timeseries - gauge distribution is never adjusted",
		[]*metricspb.Metric{gaugeDist(keys0, timeseries(100, vals0, dist(100, bounds0, []int64{4, 2, 3, 7})))},
		[]*metricspb.Metric{gaugeDist(keys0, timeseries(100, vals0, dist(100, bounds0, []int64{4, 2, 3, 7})))},
	}, {
		"GaugeDist: second timeseries - gauge distribution is never adjusted",
		[]*metricspb.Metric{gaugeDist(keys0, timeseries(150, vals0, dist(150, bounds0, []int64{6, 5, 8, 11})))},
		[]*metricspb.Metric{gaugeDist(keys0, timeseries(150, vals0, dist(150, bounds0, []int64{6, 5, 8, 11})))},
	}, {
		"GaugeDist: third timeseries - count/sum less than previous - gauge distribution is never adjusted",
		[]*metricspb.Metric{gaugeDist(keys0, timeseries(200, vals0, dist(200, bounds0, []int64{2, 0, 1, 5})))},
		[]*metricspb.Metric{gaugeDist(keys0, timeseries(200, vals0, dist(200, bounds0, []int64{2, 0, 1, 5})))},
	}}
	runScript(t, script)
}

func Test_cumulativeDouble(t *testing.T) {
	script := []*metricsAdjusterTest{{
		"CumulativeDouble: first timeseries - initial, adjusted should be empty",
		[]*metricspb.Metric{cumulative(keys0, timeseries(100, vals0, double(100, 44)))},
		[]*metricspb.Metric{},
	}, {
		"CumulativeDouble: second timeseries - adjusted based on first timeseries",
		[]*metricspb.Metric{cumulative(keys0, timeseries(150, vals0, double(150, 66)))},
		[]*metricspb.Metric{cumulative(keys0, timeseries(100, vals0, double(150, 22)))},
	}, {
		"CumulativeDouble: third timeseries - reset (value less than previous value), adjusted should be empty",
		[]*metricspb.Metric{cumulative(keys0, timeseries(200, vals0, double(200, 55)))},
		[]*metricspb.Metric{},
	}, {
		"CumulativeDouble: fourth timeseries - adjusted based on reset timeseries",
		[]*metricspb.Metric{cumulative(keys0, timeseries(250, vals0, double(250, 72)))},
		[]*metricspb.Metric{cumulative(keys0, timeseries(200, vals0, double(250, 17)))},
	}}
	runScript(t, script)
}

func Test_cumulativeDistribution(t *testing.T) {
	script := []*metricsAdjusterTest{{
		"CumulativeDist: first timeseries - initial, adjusted should be empty",
		[]*metricspb.Metric{cumulativeDist(keys0, timeseries(100, vals0, dist(100, bounds0, []int64{4, 2, 3, 7})))},
		[]*metricspb.Metric{},
	}, {
		"CumulativeDist: second timeseries - adjusted based on first timeseries",
		[]*metricspb.Metric{cumulativeDist(keys0, timeseries(150, vals0, dist(150, bounds0, []int64{6, 3, 4, 8})))},
		[]*metricspb.Metric{cumulativeDist(keys0, timeseries(100, vals0, dist(150, bounds0, []int64{2, 1, 1, 1})))},
	}, {
		"CumulativeDist: third timeseries - reset (value less than previous value), adjusted should be empty",
		[]*metricspb.Metric{cumulativeDist(keys0, timeseries(200, vals0, dist(200, bounds0, []int64{5, 3, 2, 7})))},
		[]*metricspb.Metric{},
	}, {
		"CumulativeDist: fourth timeseries - adjusted based on reset timeseries",
		[]*metricspb.Metric{cumulativeDist(keys0, timeseries(250, vals0, dist(250, bounds0, []int64{7, 4, 2, 12})))},
		[]*metricspb.Metric{cumulativeDist(keys0, timeseries(200, vals0, dist(250, bounds0, []int64{2, 1, 0, 5})))},
	}}
	runScript(t, script)
}

func Test_summary(t *testing.T) {
	script := []*metricsAdjusterTest{{
		"Summary: first timeseries - initial, adjusted should be empty",
		[]*metricspb.Metric{summary(keys0, timeseries(100, vals0, summ(100, 10, 40, percent0, []float64{1.0, 5.0, 8.0})))},
		[]*metricspb.Metric{},
	}, {
		"Summary: second timeseries - adjusted based on first timeseries",
		[]*metricspb.Metric{summary(keys0, timeseries(150, vals0, summ(150, 15, 70, percent0, []float64{7.0, 44.0, 9.0})))},
		[]*metricspb.Metric{summary(keys0, timeseries(100, vals0, summ(150, 5, 30, percent0, []float64{7.0, 44.0, 9.0})))},
	}, {
		"Summary: third timeseries - reset (count less than previous), adjusted should be empty",
		[]*metricspb.Metric{summary(keys0, timeseries(200, vals0, summ(200, 12, 66, percent0, []float64{3.0, 22.0, 5.0})))},
		[]*metricspb.Metric{},
	}, {
		"Summary: fourth timeseries - adjusted based on reset timeseries",
		[]*metricspb.Metric{summary(keys0, timeseries(250, vals0, summ(250, 14, 96, percent0, []float64{9.0, 47.0, 8.0})))},
		[]*metricspb.Metric{summary(keys0, timeseries(200, vals0, summ(250, 2, 30, percent0, []float64{9.0, 47.0, 8.0})))},
	}}
	runScript(t, script)
}

var (
	keys0    = []string{"k1", "k2"}
	vals0    = []string{"v1", "v2"}
	bounds0  = []float64{1.0, 2.0, 4.0}
	percent0 = []float64{10.0, 50.0, 90.0}
)

type metricsAdjusterTest struct {
	description string
	metrics     []*metricspb.Metric
	adjusted    []*metricspb.Metric
}

func runScript(t *testing.T, script []*metricsAdjusterTest) {
	l, _ := zap.NewProduction()
	defer l.Sync() // flushes buffer, if any
	ma := NewMetricsAdjuster(newMetricsInstanceMap(), l.Sugar())
	for _, test := range script {
		adjusted := ma.AdjustMetrics(test.metrics)
		if !reflect.DeepEqual(test.adjusted, adjusted) {
			t.Errorf("Error: %v - expected: %v, actual %v", test.description, test.adjusted, adjusted)
			break
		}
	}
}

func gauge(keys []string, timeseries ...*metricspb.TimeSeries) *metricspb.Metric {
	return metric(metricspb.MetricDescriptor_GAUGE_DOUBLE, keys, timeseries)
}

func gaugeDist(keys []string, timeseries ...*metricspb.TimeSeries) *metricspb.Metric {
	return metric(metricspb.MetricDescriptor_GAUGE_DISTRIBUTION, keys, timeseries)
}

func cumulative(keys []string, timeseries ...*metricspb.TimeSeries) *metricspb.Metric {
	return metric(metricspb.MetricDescriptor_CUMULATIVE_DOUBLE, keys, timeseries)
}

func cumulativeDist(keys []string, timeseries ...*metricspb.TimeSeries) *metricspb.Metric {
	return metric(metricspb.MetricDescriptor_CUMULATIVE_DISTRIBUTION, keys, timeseries)
}

func summary(keys []string, timeseries ...*metricspb.TimeSeries) *metricspb.Metric {
	return metric(metricspb.MetricDescriptor_SUMMARY, keys, timeseries)
}

func metric(ty metricspb.MetricDescriptor_Type, keys []string, timeseries []*metricspb.TimeSeries) *metricspb.Metric {
	return &metricspb.Metric{
		MetricDescriptor: metricDescriptor(ty, keys),
		Timeseries:       timeseries,
	}
}

func metricDescriptor(ty metricspb.MetricDescriptor_Type, keys []string) *metricspb.MetricDescriptor {
	return &metricspb.MetricDescriptor{
		Name:        ty.String(),
		Description: "description " + ty.String(),
		Unit:        "", // units not affected by adjuster
		Type:        ty,
		LabelKeys:   toKeys(keys),
	}
}

func timeseries(sts int64, vals []string, point *metricspb.Point) *metricspb.TimeSeries {
	return &metricspb.TimeSeries{
		StartTimestamp: toTS(sts),
		Points:         []*metricspb.Point{point},
		LabelValues:    toVals(vals),
	}
}

func double(ts int64, value float64) *metricspb.Point {
	return &metricspb.Point{Timestamp: toTS(ts), Value: &metricspb.Point_DoubleValue{DoubleValue: value}}
}

func dist(ts int64, bounds []float64, counts []int64) *metricspb.Point {
	var count int64 = 0
	var sum float64 = 0.0
	buckets := make([]*metricspb.DistributionValue_Bucket, len(counts))

	for i, bcount := range counts {
		count += bcount
		buckets[i] = &metricspb.DistributionValue_Bucket{Count: bcount}
		// create a sum based on lower bucket bounds
		// e.g. for bounds = {0.1, 0.2, 0.4} and counts = {2, 3, 7, 9)
		// sum = 0*2 + 0.1*3 + 0.2*7 + 0.4*9
		if i > 0 {
			sum += float64(bcount) * bounds[i-1]
		}
	}
	distrValue := &metricspb.DistributionValue{
		BucketOptions: &metricspb.DistributionValue_BucketOptions{
			Type: &metricspb.DistributionValue_BucketOptions_Explicit_{
				Explicit: &metricspb.DistributionValue_BucketOptions_Explicit{
					Bounds: bounds,
				},
			},
		},
		Count:   count,
		Sum:     sum,
		Buckets: buckets,
		// SumOfSquaredDeviation:  // there's no way to compute this value from prometheus data
	}
	return &metricspb.Point{Timestamp: toTS(ts), Value: &metricspb.Point_DistributionValue{DistributionValue: distrValue}}
}

func summ(ts, count int64, sum float64, percent, vals []float64) *metricspb.Point {
	percentiles := make([]*metricspb.SummaryValue_Snapshot_ValueAtPercentile, len(percent))
	for i := 0; i < len(percent); i++ {
		percentiles[i] = &metricspb.SummaryValue_Snapshot_ValueAtPercentile{Percentile: percent[i], Value: vals[i]}
	}
	summaryValue := &metricspb.SummaryValue{
		Sum:   &wrappers.DoubleValue{Value: sum},
		Count: &wrappers.Int64Value{Value: count},
		Snapshot: &metricspb.SummaryValue_Snapshot{
			PercentileValues: percentiles,
		},
	}
	return &metricspb.Point{Timestamp: toTS(ts), Value: &metricspb.Point_SummaryValue{SummaryValue: summaryValue}}
}

func toKeys(keys []string) []*metricspb.LabelKey {
	res := make([]*metricspb.LabelKey, 0, len(keys))
	for _, key := range keys {
		res = append(res, &metricspb.LabelKey{Key: key, Description: "description: " + key})
	}
	return res
}

func toVals(vals []string) []*metricspb.LabelValue {
	res := make([]*metricspb.LabelValue, 0, len(vals))
	for _, val := range vals {
		res = append(res, &metricspb.LabelValue{Value: val, HasValue: true})
	}
	return res
}

func toTS(timeAtMs int64) *timestamp.Timestamp {
	secs, ns := timeAtMs/1e3, (timeAtMs%1e3)*1e6
	return &timestamp.Timestamp{
		Seconds: secs,
		Nanos:   int32(ns),
	}
}

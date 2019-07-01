package internal

import (
	"fmt"
	"github.com/golang/protobuf/ptypes/wrappers"
	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
	"go.uber.org/zap"
	"strings"
)

type MetricsAdjuster struct {
	metricMap     *map[string]*metricspb.TimeSeries
	logger        *zap.SugaredLogger
}

func NewMetricsAdjuster(metricMap *map[string]*metricspb.TimeSeries, logger *zap.SugaredLogger) *MetricsAdjuster {
	return &MetricsAdjuster{
		metricMap: metricMap,
		logger:    logger,
	}
}

func (ma *MetricsAdjuster) AdjustMetrics(metrics []*metricspb.Metric) []*metricspb.Metric {
	var adjusted = make([]*metricspb.Metric, 0, len(metrics))
	for _, metric := range metrics {
		if ma.adjustMetric(metric) {
			adjusted = append(adjusted, metric)
		}
	}
	return adjusted
}

func (ma *MetricsAdjuster) adjustMetric(metric *metricspb.Metric) bool {
	switch metric.MetricDescriptor.Type {
	case metricspb.MetricDescriptor_GAUGE_DOUBLE:
		return true
	default:
		return ma.adjustMetricTimeseries(metric)
	}
}

func (ma *MetricsAdjuster) adjustMetricTimeseries(metric *metricspb.Metric) bool {
	filtered := make([]*metricspb.TimeSeries, 0, len(metric.GetTimeseries()))
	for _, timeseries := range metric.GetTimeseries() {
		key := getSignature(metric.GetMetricDescriptor().GetName(), timeseries.GetLabelValues())
		initial, ok := (*ma.metricMap)[key]
		if ok {
			if !ma.adjustTimeseries(metric.MetricDescriptor.Type, timeseries, initial) {
				// detected a reset
				(*ma.metricMap)[key] = timeseries
			}
			filtered = append(filtered, timeseries)
		} else {
			(*ma.metricMap)[key] = timeseries
		}
	}
	if len(filtered) == 0 {
		return false
	}
	metric.Timeseries = filtered
	return true
}

func (ma *MetricsAdjuster) adjustTimeseries(metricType metricspb.MetricDescriptor_Type, current, initial *metricspb.TimeSeries) bool {
	if !ma.adjustPoints(metricType, current.GetPoints(), initial.GetPoints()) {
		return false
	}
	current.StartTimestamp = initial.StartTimestamp
	return true
}

func (ma *MetricsAdjuster) adjustPoints(metricType metricspb.MetricDescriptor_Type, current, initial []*metricspb.Point) bool {
	if len(current) != len(initial) {
		// this shouldn't happpen, both lengths should always be 1.
		ma.logger.Info("len(current points) != len(initial points)", len(current), len(initial))
		return true
	}
	for i := 0; i < len(current); i++ {
		if !ma.adjustPoint(metricType, current[i], initial[i]) {
			return false
		}
	}
	return true
}

// Types of metrics returned supported by prometheus:
// - MetricDescriptor_CUMULATIVE_DOUBLE
// - MetricDescriptor_GAUGE_DOUBLE
// - MetricDescriptor_CUMULATIVE_DISTRIBUTION
// - MetricDescriptor_GAUGE_DISTRIBUTION
// - MetricDescriptor_SUMMARY
func (ma *MetricsAdjuster) adjustPoint(metricType metricspb.MetricDescriptor_Type, current, initial *metricspb.Point) bool {
	switch metricType {
	case metricspb.MetricDescriptor_CUMULATIVE_DISTRIBUTION:
		// note: sum of squared deviation not currently supported
		initialDist := initial.GetDistributionValue()
		currentDist := current.GetDistributionValue()
		if (currentDist.Count < initialDist.Count) {
			return false
		}
		currentDist.Count -= initialDist.Count
		currentDist.Sum -= initialDist.Sum
		ma.adjustBuckets(currentDist.Buckets, initialDist.Buckets)
	case metricspb.MetricDescriptor_GAUGE_DISTRIBUTION:
		// note: sum of squared deviation not currently supported
		// note: we don't adjust bucket counts for gauge distributions
		initialDist := initial.GetDistributionValue()
		currentDist := current.GetDistributionValue()
		if (currentDist.Count < initialDist.Count) {
			return false
		}
		currentDist.Count -= initialDist.Count
		currentDist.Sum -= initialDist.Sum
		ma.adjustBuckets(currentDist.Buckets, initialDist.Buckets)
	case metricspb.MetricDescriptor_SUMMARY:
		// note: for summary, we don't adjust the snapshot
		initialSummary := initial.GetSummaryValue()
		currentSummary := current.GetSummaryValue()
		initialCount := initialSummary.Count.GetValue()
		currentCount := currentSummary.Count.GetValue()
		if (currentCount < initialCount) {
			return false
		}
		initialSum := initialSummary.Sum.GetValue()
		currentSum := currentSummary.Sum.GetValue()
		currentSummary.Count = &wrappers.Int64Value{Value: currentCount - initialCount}
		currentSummary.Sum = &wrappers.DoubleValue{Value: currentSum - initialSum}
	case metricspb.MetricDescriptor_CUMULATIVE_DOUBLE:
		currentValue := current.GetDoubleValue()
		initialValue := initial.GetDoubleValue()
		if currentValue < initialValue {
			// reset detected
			return false
		}
		current.Value = &metricspb.Point_DoubleValue{DoubleValue: currentValue - initialValue}
	default:
		// this shouldn't happen
		ma.logger.Info("adjust unexpect point type ", metricType.String(), ", skipping ...")
	}
	return true
}

func (ma *MetricsAdjuster) adjustBuckets(current, initial []*metricspb.DistributionValue_Bucket) bool {
	if (len(current) != len(initial)) {
		// this shouldn't happen
		ma.logger.Info("len(current buckets) != len(initial buckets)", len(current), len(initial))
		return false
	}
	for i := 0; i < len(current); i++ {
		current[i].Count -= initial[i].Count
	}
	return true
}
	

// creates a unique signature consisting of a metric's name and label values
func getSignature(name string, values []*metricspb.LabelValue) string {
	labelValues := make([]string, 0, len(values))
	for _, label := range values {
		labelValues = append(labelValues, label.GetValue())
	}
	return fmt.Sprintf("%s,%s", name, strings.Join(labelValues, ","))
}

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

type timeseriesinfo struct {
	initial  *metricspb.TimeSeries
	previous *metricspb.TimeSeries
}

type metricsInstanceMap struct {
	lastAccess time.Time
	internal   map[string]*timeseriesinfo
}

func newMetricsInstanceMap() *metricsInstanceMap {
	return &metricsInstanceMap{internal: make(map[string]*timeseriesinfo)}
}

func (mim *metricsInstanceMap) get(metric *metricspb.Metric, values []*metricspb.LabelValue) *timeseriesinfo {
	mim.lastAccess = time.Now()
	name := metric.GetMetricDescriptor().GetName()
	sig := getSignature(name, values)
	tsi, ok := mim.internal[sig]
	if !ok {
		tsi = &timeseriesinfo{}
		mim.internal[sig] = tsi
	}
	return tsi
}

// create a unique signature consisting of a metric's name and label values
func getSignature(name string, values []*metricspb.LabelValue) string {
	labelValues := make([]string, 0, len(values))
	for _, label := range values {
		if label.GetValue() != "" {
			labelValues = append(labelValues, label.GetValue())
		}
	}
	return fmt.Sprintf("%s,%s", name, strings.Join(labelValues, ","))
}

// JobsMap maps from a job instance to a map of metric instances for the job.
type JobsMap struct {
	sync.RWMutex
	lastGC   time.Time
	internal map[string]*metricsInstanceMap
}

const jobsMapGCIntervalInSeconds = 600
const jobMaxLifetimeInSeconds = 600

// NewJobsMap creates a new (empty) JobsMap.
func NewJobsMap() *JobsMap {
	return &JobsMap{lastGC: time.Now(), internal: make(map[string]*metricsInstanceMap)}
}

func (jm *JobsMap) maybeGC(lastGC time.Time) {
	current := time.Now()
	if current.Sub(lastGC).Seconds() > jobsMapGCIntervalInSeconds {
		go func() {
			jm.Lock()
			if current.Sub(jm.lastGC).Seconds() > jobsMapGCIntervalInSeconds {
				for sig, mim := range jm.internal {
					if current.Sub(mim.lastAccess).Seconds() > jobMaxLifetimeInSeconds {
						delete(jm.internal, sig)
					}
				}
				jm.lastGC = time.Now()
			}
			jm.Unlock()
		}()
	}
}

func (jm *JobsMap) get(job, instance string) *metricsInstanceMap {
	sig := job + ":" + instance
	jm.RLock()
	lastGC := jm.lastGC
	mim, ok := jm.internal[sig]
	jm.RUnlock()
	defer jm.maybeGC(lastGC)
	if ok {
		return mim
	}
	jm.Lock()
	defer jm.Unlock()
	mim2, ok2 := jm.internal[sig]
	if ok2 {
		return mim2
	}
	mim2 = newMetricsInstanceMap()
	jm.internal[sig] = mim2
	return mim2
}

// MetricsAdjuster takes a map from a metric instance to the initial point in the metrics instance
// and provides AdjustMetrics, which takes a sequence of metrics and adjust their values based on
// the initial points.
type MetricsAdjuster struct {
	mim    *metricsInstanceMap
	logger *zap.SugaredLogger
}

// NewMetricsAdjuster is a constructor for MetricsAdjuster.
func NewMetricsAdjuster(mim *metricsInstanceMap, logger *zap.SugaredLogger) *MetricsAdjuster {
	return &MetricsAdjuster{
		mim:    mim,
		logger: logger,
	}
}

// AdjustMetrics takes a sequence of metrics and adjust their values based on the initial and previous points in the
// metricsInstanceMap. If the metric is the first point in the timeseries, or the timeseries has been reset, it is
// removed from the sequence and added to the the metricsInstanceMap.
func (ma *MetricsAdjuster) AdjustMetrics(metrics []*metricspb.Metric) []*metricspb.Metric {
	var adjusted = make([]*metricspb.Metric, 0, len(metrics))
	for _, metric := range metrics {
		if ma.adjustMetric(metric) {
			adjusted = append(adjusted, metric)
		}
	}
	return adjusted
}

// returns true if at least one of the metric's timeseries was adjusted and false if all of the timeseries are an initial occurence or a reset.
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

// Returns true if at least one of the metric's timeseries was adjusted and false if all of the timeseries are an initial occurence or a reset.
func (ma *MetricsAdjuster) adjustMetricTimeseries(metric *metricspb.Metric) bool {
	filtered := make([]*metricspb.TimeSeries, 0, len(metric.GetTimeseries()))
	for _, current := range metric.GetTimeseries() {
		tsi := ma.mim.get(metric, current.GetLabelValues())
		if tsi.initial == nil {
			// initial timeseries
			tsi.initial = current
			tsi.previous = current
		} else {
			if ma.adjustTimeseries(metric.MetricDescriptor.Type, current, tsi.initial, tsi.previous) {
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

// returns true if 'current' was adjusted and false if 'current' is an the initial occurence or a reset of the timeseries.
func (ma *MetricsAdjuster) adjustTimeseries(metricType metricspb.MetricDescriptor_Type, current, initial, previous *metricspb.TimeSeries) bool {
	if !ma.adjustPoints(metricType, current.GetPoints(), initial.GetPoints(), previous.GetPoints()) {
		return false
	}
	current.StartTimestamp = initial.StartTimestamp
	return true
}

func (ma *MetricsAdjuster) adjustPoints(metricType metricspb.MetricDescriptor_Type, current, initial, previous []*metricspb.Point) bool {
	if len(current) != 1 || len(initial) != 1 || len(current) != 1 {
		ma.logger.Infof("len(current): %v, len(initial): %v, len(previous): %v should all be 1", len(current), len(initial), len(previous))
		return true
	}
	return ma.adjustPoint(metricType, current[0], initial[0], previous[0])
}

// Note: There is an important, subtle point here. When a new timeseries or a reset is detected, current and initial are the same object.
// When initial == previous, the previous value/count/sum are all the initial value. When initial != previous, the previous value/count/sum has
// been adjusted wrt the initial value so both they must be combined to find the actual previous value/count/sum. This happens because the
// timeseries are updated in-place - if new copies of the timeseries were created instead, previous could be used directly but this would
// mean reallocating all of the metrics.
func (ma *MetricsAdjuster) adjustPoint(metricType metricspb.MetricDescriptor_Type, current, initial, previous *metricspb.Point) bool {
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
		current.Value = &metricspb.Point_DoubleValue{DoubleValue: currentValue - initialValue}
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
		current.GetSummaryValue().Count = &wrappers.Int64Value{Value: currentCount - initialCount}
		current.GetSummaryValue().Sum = &wrappers.DoubleValue{Value: currentSum - initialSum}
	default:
		// this shouldn't happen
		ma.logger.Infof("adjust unexpect point type %v, skipping ...", metricType.String())
	}
	return true
}

func (ma *MetricsAdjuster) adjustBuckets(current, initial []*metricspb.DistributionValue_Bucket) {
	if len(current) != len(initial) {
		// this shouldn't happen
		ma.logger.Infof("len(current buckets): %v != len(initial buckets): %v", len(current), len(initial))
	}
	for i := 0; i < len(current); i++ {
		current[i].Count -= initial[i].Count
	}
}

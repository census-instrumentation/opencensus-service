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

package processor

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.uber.org/zap"

	"github.com/census-instrumentation/opencensus-service/internal/collector/processor/idbatcher"
	"github.com/census-instrumentation/opencensus-service/internal/collector/sampling"
	"github.com/census-instrumentation/opencensus-service/internal/collector/telemetry"
)

// Policy combines a sampling policy evaluator with the destinations to be
// used for that policy.
type Policy struct {
	// Name used to identify this policy instance.
	Name string
	// Evaluator that decides if a trace is sampled or not by this policy instance.
	Evaluator sampling.PolicyEvaluator
	// Destination is the consumer of the traces selected to be sampled.
	Destination SpanProcessor
	// ctx used to carry metric tags of each policy.
	ctx context.Context
}

// traceKey is defined since sync.Map requires a comparable type, isolating it on its own
// type to help track usage.
type traceKey string

// tailSamplingSpanProcessor handles the incoming trace data and uses the given sampling
// policies to sample traces.
type tailSamplingSpanProcessor struct {
	ctx             context.Context
	start           sync.Once
	maxNumTraces    uint64
	policies        []*Policy
	logger          *zap.Logger
	idToTrace       sync.Map
	policyTicker    tTicker
	decisionBatcher idbatcher.Batcher
	deleteChan      chan traceKey
	numTracesOnMap  uint64
}

var _ SpanProcessor = (*tailSamplingSpanProcessor)(nil)

// NewTailSamplingSpanProcessor creates a TailSamplingSpanProcessor with the given policies.
// It will keep maxNumTraces on memory and will attempt to wait until decisionWait before evaluating if
// a trace should be sampled or not. Providing expectedNewTracesPerSec helps with allocating data structures
// with closer to actual usage size.
func NewTailSamplingSpanProcessor(
	policies []*Policy,
	maxNumTraces, expectedNewTracesPerSec uint64,
	decisionWait time.Duration,
	logger *zap.Logger) (SpanProcessor, error) {

	numDecisionBatches := uint64(decisionWait.Seconds())
	inBatcher, err := idbatcher.New(numDecisionBatches, expectedNewTracesPerSec, uint64(2*runtime.NumCPU()))
	if err != nil {
		return nil, err
	}
	tsp := &tailSamplingSpanProcessor{
		ctx:             context.Background(),
		maxNumTraces:    maxNumTraces,
		policies:        policies,
		logger:          logger,
		decisionBatcher: inBatcher,
	}

	for _, policy := range policies {
		mutator := tag.Insert(tagPolicyKey, policy.Name)
		policyCtx, err := tag.New(tsp.ctx, mutator)
		if err != nil {
			return nil, err
		}
		policy.ctx = policyCtx
	}
	tsp.policyTicker = &policyTicker{onTick: tsp.samplingPolicyOnTick}
	tsp.deleteChan = make(chan traceKey, maxNumTraces)
	return tsp, nil
}

func (tsp *tailSamplingSpanProcessor) samplingPolicyOnTick() {
	var idNotFoundOnMapCount, evaluateErrorCount, decisionSampled, decisionNotSampled int64
	startTime := time.Now()
	batch, _ := tsp.decisionBatcher.CloseCurrentAndTakeFirstBatch()
	batchLen := len(batch)
	tsp.logger.Debug("Sampling Policy Evaluation ticked")
	for _, id := range batch {
		d, ok := tsp.idToTrace.Load(traceKey(id))
		if !ok {
			idNotFoundOnMapCount++
			continue
		}
		trace := d.(*sampling.TraceData)
		trace.DecisionTime = time.Now()
		for i, policy := range tsp.policies {
			policyEvaluateStartTime := time.Now()
			decision, err := policy.Evaluator.Evaluate(id, trace)
			stats.Record(
				policy.ctx,
				statDecisionLatencyMicroSec.M(int64(time.Since(policyEvaluateStartTime)/time.Microsecond)))
			if err != nil {
				trace.Decision[i] = sampling.NotSampled
				evaluateErrorCount++
				tsp.logger.Error("Sampling policy error", zap.Error(err))
				continue
			}

			trace.Decision[i] = decision

			switch decision {
			case sampling.Sampled:
				decisionSampled++

				trace.Lock()
				traceBatches := trace.ReceivedBatches
				trace.Unlock()

				for j := 0; j < len(traceBatches); j++ {
					policy.Destination.ProcessSpans(traceBatches[j], "tail-sampling")
				}
			case sampling.NotSampled:
				decisionNotSampled++
			}
		}

		// Sampled or not, remove the batches
		trace.Lock()
		trace.ReceivedBatches = nil
		trace.Unlock()
	}

	stats.Record(tsp.ctx,
		statOverallDecisionLatencyµs.M(int64(time.Since(startTime)/time.Microsecond)),
		statDroppedTooEarlyCount.M(idNotFoundOnMapCount),
		statPolicyEvaluationErrorCount.M(evaluateErrorCount),
		statTracesOnMemoryGauge.M(int64(atomic.LoadUint64(&tsp.numTracesOnMap))))

	tsp.logger.Debug("Sampling policy evaluation completed",
		zap.Int("batch.len", batchLen),
		zap.Int64("sampled", decisionSampled),
		zap.Int64("notSampled", decisionNotSampled),
		zap.Int64("droppedPriorToEvaluation", idNotFoundOnMapCount),
		zap.Int64("policyEvaluationErrors", evaluateErrorCount),
	)
}

// ProcessSpans is required by the SpanProcessor interface.
func (tsp *tailSamplingSpanProcessor) ProcessSpans(batch *agenttracepb.ExportTraceServiceRequest, spanFormat string) (uint64, error) {
	tsp.start.Do(func() {
		tsp.logger.Info("First trace data arrived, starting tail-sampling timers")
		tsp.policyTicker.Start(1 * time.Second)
	})

	// Groupd spans per their traceId to minize contention on idToTrace
	idToSpans := make(map[traceKey][]*tracepb.Span)
	for _, span := range batch.Spans {
		if len(span.TraceId) != 16 {
			tsp.logger.Warn("Span without valid TraceId", zap.String("spanFormat", spanFormat))
			continue
		}
		traceKey := traceKey(span.TraceId)
		idToSpans[traceKey] = append(idToSpans[traceKey], span)
	}

	var newTraceIDs int64
	singleTrace := len(idToSpans) == 1
	for id, spans := range idToSpans {
		lenSpans := int64(len(spans))
		lenPolicies := len(tsp.policies)
		initialDecisions := make([]sampling.Decision, lenPolicies, lenPolicies)
		for i := 0; i < lenPolicies; i++ {
			initialDecisions[i] = sampling.Pending
		}
		initialTraceData := &sampling.TraceData{
			Decision:    initialDecisions,
			ArrivalTime: time.Now(),
			SpanCount:   lenSpans,
		}
		d, loaded := tsp.idToTrace.LoadOrStore(traceKey(id), initialTraceData)

		actualData := d.(*sampling.TraceData)
		if loaded {
			atomic.AddInt64(&actualData.SpanCount, lenSpans)
		} else {
			newTraceIDs++
			tsp.decisionBatcher.AddToCurrentBatch([]byte(id))
			atomic.AddUint64(&tsp.numTracesOnMap, 1)
			postDeletion := false
			currTime := time.Now()
			for !postDeletion {
				select {
				case tsp.deleteChan <- id:
					postDeletion = true
				default:
					traceKeyToDrop := <-tsp.deleteChan
					tsp.dropTrace(traceKeyToDrop, currTime)
				}
			}
		}

		for i, policyAndDests := range tsp.policies {
			actualData.Lock()
			actualDecision := actualData.Decision[i]
			// If decision is pending, we want to add the new spans still under the lock, so the decision doesn't happen
			// in between the transition from pending.
			if actualDecision == sampling.Pending {
				// Add the spans to the trace, but only once for all policies, otherwise same spans will
				// be duplicated in the final trace.
				traceBatch := prepareTraceBatch(spans, singleTrace, batch)
				actualData.ReceivedBatches = append(actualData.ReceivedBatches, traceBatch)
				actualData.Unlock()
				break
			}
			actualData.Unlock()

			switch actualDecision {
			case sampling.Pending:
				// All process for pending done above, keep the case so it doesn't go to default.
			case sampling.Sampled:
				// Forward the spans to the policy destinations
				traceBatch := prepareTraceBatch(spans, singleTrace, batch)
				if failCount, err := policyAndDests.Destination.ProcessSpans(traceBatch, spanFormat); err != nil {
					tsp.logger.Warn("Error sending late arrived spans to destination",
						zap.String("policy", policyAndDests.Name),
						zap.Uint64("failCount", failCount),
						zap.Error(err))
				}
				fallthrough // so OnLateArrivingSpans is also called for decision Sampled.
			case sampling.NotSampled:
				policyAndDests.Evaluator.OnLateArrivingSpans(actualDecision, spans)
				stats.Record(tsp.ctx, statLateSpanArrivalAfterDecision.M(int64(time.Since(actualData.DecisionTime)/time.Second)))

			default:
				tsp.logger.Warn("Encountered unexpected sampling decision",
					zap.String("policy", policyAndDests.Name),
					zap.Int("decision", int(actualDecision)))
			}
		}
	}

	stats.Record(tsp.ctx, statNewTraceIDReceivedCount.M(newTraceIDs))
	return 0, nil
}

func (tsp *tailSamplingSpanProcessor) dropTrace(traceID traceKey, deletionTime time.Time) {
	var trace *sampling.TraceData
	if d, ok := tsp.idToTrace.Load(traceID); ok {
		trace = d.(*sampling.TraceData)
		tsp.idToTrace.Delete(traceID)
		// Subtract one from numTracesOnMap per https://godoc.org/sync/atomic#AddUint64
		atomic.AddUint64(&tsp.numTracesOnMap, ^uint64(0))
	}
	if trace == nil {
		tsp.logger.Error("Attempt to delete traceID not on table")
		return
	}
	policiesLen := len(tsp.policies)
	stats.Record(tsp.ctx, statTraceRemovalAgeSec.M(int64(deletionTime.Sub(trace.ArrivalTime)/time.Second)))
	for j := 0; j < policiesLen; j++ {
		if trace.Decision[j] == sampling.Pending {
			policy := tsp.policies[j]
			if decision, err := policy.Evaluator.OnDroppedSpans([]byte(traceID), trace); err != nil {
				tsp.logger.Warn("OnDroppedSpans",
					zap.String("policy", policy.Name),
					zap.Int("decision", int(decision)),
					zap.Error(err))
			}
		}
	}
}

func prepareTraceBatch(spans []*tracepb.Span, singleTrace bool, batch *agenttracepb.ExportTraceServiceRequest) *agenttracepb.ExportTraceServiceRequest {
	var traceBatch *agenttracepb.ExportTraceServiceRequest
	if singleTrace {
		// Special case no need to prepare a batch
		traceBatch = batch
	} else {
		traceBatch = &agenttracepb.ExportTraceServiceRequest{
			Node:     batch.Node,
			Resource: batch.Resource,
			Spans:    spans,
		}
	}
	return traceBatch
}

// tTicker interface allows easier testing of ticker related functionality used by tailSamplingProcessor
type tTicker interface {
	// Start sets the frequency of the ticker and starts the perioc calls to OnTick.
	Start(d time.Duration)
	// OnTick is called when the ticker fires.
	OnTick()
	// Stops firing the ticker.
	Stop()
}

type policyTicker struct {
	ticker *time.Ticker
	onTick func()
}

func (pt *policyTicker) Start(d time.Duration) {
	pt.ticker = time.NewTicker(d)
	go func() {
		for range pt.ticker.C {
			pt.OnTick()
		}
	}()
}
func (pt *policyTicker) OnTick() {
	pt.onTick()
}
func (pt *policyTicker) Stop() {
	pt.ticker.Stop()
}

var _ tTicker = (*policyTicker)(nil)

// Variables related to metrics specific to queued processor.
var (
	tagPolicyKey, _ = tag.NewKey("policy")

	statDecisionLatencyMicroSec  = stats.Int64("sampling_decision_latency", "Latency (in microseconds) of a given sampling policy", "µs")
	statOverallDecisionLatencyµs = stats.Int64("sampling_decision_timer_latency", "Latency (in microseconds) of each run of the sampling decision timer", "µs")

	statTraceRemovalAgeSec           = stats.Int64("sampling_trace_removal_age", "Time (in seconds) from arrival of a new trace until its removal from memory", "s")
	statLateSpanArrivalAfterDecision = stats.Int64("sampling_late_span_age", "Time (in seconds) from the sampling decision was taken and the arrival of a late span", "s")

	statPolicyEvaluationErrorCount = stats.Int64("sampling_policy_evaluation_error", "Count of sampling policy evaluation errors", stats.UnitDimensionless)

	statDroppedTooEarlyCount    = stats.Int64("sampling_trace_dropped_too_early", "Count of traces that needed to be dropped the configured wait time", stats.UnitDimensionless)
	statNewTraceIDReceivedCount = stats.Int64("new_trace_id_received", "Counts the arrival of new traces", stats.UnitDimensionless)
	statTracesOnMemoryGauge     = stats.Int64("sampling_traces_on_memory", "Tracks the number of traces current on memory", stats.UnitDimensionless)
)

// SamplingProcessorMetricViews return the metrics views according to given telemetry level.
func SamplingProcessorMetricViews(level telemetry.Level) []*view.View {
	if level == telemetry.None {
		return nil
	}

	policyTagKeys := []tag.Key{tagPolicyKey}

	latencyDistributionAggregation := view.Distribution(1, 2, 5, 10, 25, 50, 75, 100, 150, 200, 300, 400, 500, 750, 1000, 2000, 3000, 4000, 5000, 10000, 20000, 30000, 50000)
	ageDistributionAggregation := view.Distribution(1, 2, 5, 10, 20, 30, 40, 50, 60, 90, 120, 180, 300, 600, 1800, 3600, 7200)

	decisionLatencyView := &view.View{
		Name:        statDecisionLatencyMicroSec.Name(),
		Measure:     statDecisionLatencyMicroSec,
		Description: statDecisionLatencyMicroSec.Description(),
		TagKeys:     policyTagKeys,
		Aggregation: latencyDistributionAggregation,
	}
	overallDecisionLatencyView := &view.View{
		Name:        statOverallDecisionLatencyµs.Name(),
		Measure:     statOverallDecisionLatencyµs,
		Description: statOverallDecisionLatencyµs.Description(),
		Aggregation: latencyDistributionAggregation,
	}
	traceRemovalAgeView := &view.View{
		Name:        statTraceRemovalAgeSec.Name(),
		Measure:     statTraceRemovalAgeSec,
		Description: statTraceRemovalAgeSec.Description(),
		Aggregation: ageDistributionAggregation,
	}
	lateSpanArrivalView := &view.View{
		Name:        statLateSpanArrivalAfterDecision.Name(),
		Measure:     statLateSpanArrivalAfterDecision,
		Description: statLateSpanArrivalAfterDecision.Description(),
		Aggregation: ageDistributionAggregation,
	}
	countPolicyEvaluationErrorView := &view.View{
		Name:        statPolicyEvaluationErrorCount.Name(),
		Measure:     statPolicyEvaluationErrorCount,
		Description: statPolicyEvaluationErrorCount.Description(),
		Aggregation: view.Sum(),
	}
	countTraceDroppedTooEarlyView := &view.View{
		Name:        statDroppedTooEarlyCount.Name(),
		Measure:     statDroppedTooEarlyCount,
		Description: statDroppedTooEarlyCount.Description(),
		Aggregation: view.Sum(),
	}
	countTraceIDArrivalView := &view.View{
		Name:        statNewTraceIDReceivedCount.Name(),
		Measure:     statNewTraceIDReceivedCount,
		Description: statNewTraceIDReceivedCount.Description(),
		Aggregation: view.Sum(),
	}
	trackTracesOnMemorylView := &view.View{
		Name:        statTracesOnMemoryGauge.Name(),
		Measure:     statTracesOnMemoryGauge,
		Description: statTracesOnMemoryGauge.Description(),
		Aggregation: view.LastValue(),
	}

	return []*view.View{
		decisionLatencyView,
		overallDecisionLatencyView,
		traceRemovalAgeView,
		lateSpanArrivalView,
		countPolicyEvaluationErrorView,
		countTraceDroppedTooEarlyView,
		countTraceIDArrivalView,
		trackTracesOnMemorylView}
}

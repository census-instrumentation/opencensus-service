// Copyright 2018, OpenCensus Authors
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
	"time"

	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	"github.com/jaegertracing/jaeger/pkg/queue"
	"go.uber.org/zap"
)

type queuedSpanProcessor struct {
	queue *queue.BoundedQueue
	/* TODO: (@pauloja) not doing metrics for now
	metrics                  *cApp.SpanProcessorMetrics
	batchMetrics             *processorBatchMetrics
	*/
	logger                   *zap.Logger
	sender                   SpanProcessor
	numWorkers               int
	retryOnProcessingFailure bool
	backoffDelay             time.Duration
	stopCh                   chan struct{}
}

var _ SpanProcessor = (*queuedSpanProcessor)(nil)

type queueItem struct {
	queuedTime time.Time
	batch      *agenttracepb.ExportTraceServiceRequest
	spanFormat string
}

// NewQueuedSpanProcessor returns a span processor that maintains a bounded
// in-memory queue of span batches, and sends out span batches using the
// provided sender
func NewQueuedSpanProcessor(sender SpanProcessor, opts ...Option) SpanProcessor {
	sp := newQueuedSpanProcessor(sender, opts...)

	sp.queue.StartConsumers(sp.numWorkers, func(item interface{}) {
		value := item.(*queueItem)
		sp.processItemFromQueue(value)
	})

	return sp
}

func newQueuedSpanProcessor(
	sender SpanProcessor,
	opts ...Option,
) *queuedSpanProcessor {
	options := Options.apply(opts...)
	boundedQueue := queue.NewBoundedQueue(options.queueSize, func(item interface{}) {})
	return &queuedSpanProcessor{
		queue:                    boundedQueue,
		logger:                   options.logger,
		numWorkers:               options.numWorkers,
		sender:                   sender,
		retryOnProcessingFailure: options.retryOnProcessingFailure,
		backoffDelay:             options.backoffDelay,
		stopCh:                   make(chan struct{}),
	}
}

// Stop halts the span processor and all its go-routines.
func (sp *queuedSpanProcessor) Stop() {
	close(sp.stopCh)
	sp.queue.Stop()
}

// ProcessSpans implements the SpanProcessor interface
func (sp *queuedSpanProcessor) ProcessSpans(batch *agenttracepb.ExportTraceServiceRequest, spanFormat string) (uint64, error) {
	ok := sp.enqueueSpanBatch(batch, spanFormat)
	if !ok {
		return uint64(len(batch.Spans)), nil
	}
	return 0, nil
}

func (sp *queuedSpanProcessor) enqueueSpanBatch(batch *agenttracepb.ExportTraceServiceRequest, spanFormat string) bool {
	item := &queueItem{
		queuedTime: time.Now(),
		batch:      batch,
		spanFormat: spanFormat,
	}
	addedToQueue := sp.queue.Produce(item)
	if !addedToQueue {
		sp.onItemDropped(item)
	}
	return addedToQueue
}

func (sp *queuedSpanProcessor) processItemFromQueue(item *queueItem) {
	// TODO: @(pjanotti) metrics: startTime := time.Now()
	// TODO:
	_, err := sp.sender.ProcessSpans(item.batch, item.spanFormat)
	if err != nil {
		batchSize := len(item.batch.Spans)
		if !sp.retryOnProcessingFailure {
			// throw away the batch
			sp.logger.Error("Failed to process batch, discarding", zap.Int("batch-size", batchSize))
			sp.onItemDropped(item)
		} else {
			// try to put it back at the end of queue for retry at a later time
			addedToQueue := sp.queue.Produce(item)
			if !addedToQueue {
				sp.logger.Error("Failed to process batch and failed to re-enqueue", zap.Int("batch-size", batchSize))
				sp.onItemDropped(item)
			} else {
				sp.logger.Warn("Failed to process batch, re-enqueued", zap.Int("batch-size", batchSize))
			}
		}
		// back-off for configured delay, but get interrupted when shutting down
		if sp.backoffDelay > 0 {
			sp.logger.Warn("Backing off before next attempt",
				zap.Duration("backoff-delay", sp.backoffDelay))
			select {
			case <-sp.stopCh:
				sp.logger.Info("Interrupted due to shutdown")
				break
			case <-time.After(sp.backoffDelay):
				sp.logger.Info("Resume processing")
				break
			}
		}
	}
}

func (sp *queuedSpanProcessor) onItemDropped(item *queueItem) {
	sp.logger.Warn("Span batch dropped",
		zap.Int("#spans", len(item.batch.Spans)),
		zap.String("spanSource", item.spanFormat))
}

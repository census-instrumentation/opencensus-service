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

// Package processor is the central point on the collector processing: it
// aggregates and performs any operation that applies to all traces in the
// pipeline. Traces reach it after being converted to the OpenCensus protobuf
// format.
package processor

import (
	"go.uber.org/zap"

	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
)

// SpanProcessor handles batches of spans converted to OpenCensus proto format.
type SpanProcessor interface {
	// ProcessSpans processes spans and return with either a list of true/false success or an error
	ProcessSpans(batch *agenttracepb.ExportTraceServiceRequest, spanFormat string) ([]bool, error)
}

// An initial processor that performs no operation.
type noopSpanProcessor struct{ logger *zap.Logger }

func (sp *noopSpanProcessor) ProcessSpans(batch *agenttracepb.ExportTraceServiceRequest, spanFormat string) ([]bool, error) {
	responses := make([]bool, len(batch.Spans))
	for i := 0; i < len(batch.Spans); i++ {
		responses[i] = true
	}

	sp.logger.Debug("noopSpanProcessor", zap.String("originalFormat", spanFormat), zap.Int("oks", len(responses)))
	return responses, nil
}

// NewNoopSpanProcessor creates an OC SpanProcessor that just drops the received data.
func NewNoopSpanProcessor(logger *zap.Logger) SpanProcessor {
	return &noopSpanProcessor{logger: logger}
}

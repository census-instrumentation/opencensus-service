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

	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/internal"
)

// NewMultiMetricsProcessor wraps multiple metrics exporters in a single one.
func NewMultiMetricsProcessor(mps []MetricsProcessor) MetricsProcessor {
	return metricsProcessors(mps)
}

type metricsProcessors []MetricsProcessor

var _ MetricsProcessor = (*metricsProcessors)(nil)

// ExportMetricsData exports the MetricsData to all exporters wrapped by the current one.
func (mps metricsProcessors) ProcessMetricsData(ctx context.Context, md data.MetricsData) error {
	var errs []error
	for _, mdp := range mps {
		if err := mdp.ProcessMetricsData(ctx, md); err != nil {
			errs = append(errs, err)
		}
	}
	return internal.CombineErrors(errs)
}

// NewMultiTraceProcessor wraps multiple trace exporters in a single one.
func NewMultiTraceProcessor(tps []TraceProcessor) TraceProcessor {
	return traceProcessors(tps)
}

type traceProcessors []TraceProcessor

var _ TraceProcessor = (*traceProcessors)(nil)

// ExportSpans exports the span data to all trace exporters wrapped by the current one.
func (tps traceProcessors) ProcessTraceData(ctx context.Context, td data.TraceData) error {
	var errs []error
	for _, tdp := range tps {
		if err := tdp.ProcessTraceData(ctx, td); err != nil {
			errs = append(errs, err)
		}
	}
	return internal.CombineErrors(errs)
}

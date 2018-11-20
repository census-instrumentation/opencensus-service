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
	"context"

	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/exporter"
)

type exporterSpanProcessor struct {
	tes exporter.TraceExporterSink
}

var _ SpanProcessor = (*exporterSpanProcessor)(nil)

// NewTraceExporterProcessor creates processor that feeds SpanData to the given trace exporters.
func NewTraceExporterProcessor(traceExporters ...exporter.TraceExporter) SpanProcessor {
	return &exporterSpanProcessor{tes: exporter.MultiTraceExporters(traceExporters...)}
}

func (sp *exporterSpanProcessor) ProcessSpans(batch *agenttracepb.ExportTraceServiceRequest, spanFormat string) (uint64, error) {
	ack, err := sp.tes.ReceiveTraceData(context.Background(), data.TraceData{Node: batch.Node, Resource: batch.Resource, Spans: batch.Spans})
	if err != nil {
		return ack.DroppedSpans, err
	}
	return 0, nil
}

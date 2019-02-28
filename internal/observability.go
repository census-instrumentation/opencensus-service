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

package internal

// This file contains helpers that are useful to add observability
// with metrics and tracing using OpenCensus to the various pieces
// of the service.

import (
	"context"

	"google.golang.org/grpc"

	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
)

var (
	tagKeyTraceReceiverName, _ = tag.NewKey("opencensus_trace_receiver")
	mReceiverReceivedSpans     = stats.Int64("oc.io/receiver/received_spans", "Counts the number of spans received by the receiver", "1")
	mReceiverDroppedSpans      = stats.Int64("oc.io/receiver/dropped_spans", "Counts the number of spans dropped by the receiver", "1")

	tagKeyTraceExporterName, _ = tag.NewKey("opencensus_trace_exporter")
	mExporterReceivedSpans     = stats.Int64("oc.io/exporter/received_spans", "Counts the number of spans received by the exporter", "1")
	mExporterDroppedSpans      = stats.Int64("oc.io/exporter/dropped_spans", "Counts the number of spans received by the exporter", "1")
)

// ViewReceiverReceivedSpans defines the view for the receiver received spans metric.
var ViewReceiverReceivedSpans = &view.View{
	Name:        mReceiverReceivedSpans.Name(),
	Description: mReceiverReceivedSpans.Description(),
	Measure:     mReceiverReceivedSpans,
	Aggregation: view.Sum(),
	TagKeys:     []tag.Key{tagKeyTraceReceiverName},
}

// ViewReceiverDroppedSpans defines the view for the receiver dropped spans metric.
var ViewReceiverDroppedSpans = &view.View{
	Name:        mReceiverDroppedSpans.Name(),
	Description: mReceiverDroppedSpans.Description(),
	Measure:     mReceiverDroppedSpans,
	Aggregation: view.Sum(),
	TagKeys:     []tag.Key{tagKeyTraceReceiverName},
}

// ViewExporterReceivedSpans defines the view for the exporter received spans metric.
var ViewExporterReceivedSpans = &view.View{
	Name:        mExporterReceivedSpans.Name(),
	Description: mExporterReceivedSpans.Description(),
	Measure:     mExporterReceivedSpans,
	Aggregation: view.Sum(),
	TagKeys:     []tag.Key{tagKeyTraceReceiverName, tagKeyTraceExporterName},
}

// ViewExporterDroppedSpans defines the view for the exporter dropped spans metric.
var ViewExporterDroppedSpans = &view.View{
	Name:        mExporterReceivedSpans.Name(),
	Description: mExporterReceivedSpans.Description(),
	Measure:     mExporterReceivedSpans,
	Aggregation: view.Sum(),
	TagKeys:     []tag.Key{tagKeyTraceReceiverName, tagKeyTraceExporterName},
}

// AllViews has the views for the metrics provided by the agent.
var AllViews = []*view.View{
	ViewReceiverReceivedSpans,
	ViewReceiverDroppedSpans,
	ViewExporterReceivedSpans,
	ViewExporterDroppedSpans,
}

// ContextWithTraceReceiverName adds the tag "opencensus_trace_receiver" and the name of the
// receiver as the value, and returns the newly created context.
func ContextWithTraceReceiverName(ctx context.Context, traceReceiverName string) context.Context {
	ctx, _ = tag.New(ctx, tag.Upsert(tagKeyTraceReceiverName, traceReceiverName))
	return ctx
}

// RecordTraceReceiverMetrics records the number of the spans received and dropped by the receiver.
// Use it with a context.Context generated using ContextWithTraceReceiverName().
func RecordTraceReceiverMetrics(ctxWithTraceReceiverName context.Context, receivedSpans int, droppedSpans int) {
	stats.Record(ctxWithTraceReceiverName, mReceiverReceivedSpans.M(int64(receivedSpans)), mReceiverDroppedSpans.M(int64(droppedSpans)))
}

// ContextWithTraceExporterName adds the tag "opencensus_trace_exporter" and the name of the
// receiver as the value, and returns the newly created context.
func ContextWithTraceExporterName(ctx context.Context, traceReceiverName string) context.Context {
	ctx, _ = tag.New(ctx, tag.Upsert(tagKeyTraceReceiverName, traceReceiverName))
	return ctx
}

// RecordTraceExporterMetrics records the number of the spans received and dropped by the exporter.
// Use it with a context.Context generated using ContextWithTraceExporterName().
func RecordTraceExporterMetrics(ctx context.Context, receivedSpans int, droppedSpans int) {
	stats.Record(ctx, mExporterReceivedSpans.M(int64(receivedSpans)), mExporterDroppedSpans.M(int64(droppedSpans)))
}

// GRPCServerWithObservabilityEnabled creates a gRPC server that at a bare minimum has
// the OpenCensus ocgrpc server stats handler enabled for tracing and stats.
// Use it instead of invoking grpc.NewServer directly.
func GRPCServerWithObservabilityEnabled(extraOpts ...grpc.ServerOption) *grpc.Server {
	opts := append(extraOpts, grpc.StatsHandler(&ocgrpc.ServerHandler{}))
	return grpc.NewServer(opts...)
}

// SetParentLink tries to retrieve a span from sideCtx and if one exists
// sets its SpanID, TraceID as a link in the span provided. It returns
// true only if it retrieved a parent span from the context.
func SetParentLink(sideCtx context.Context, span *trace.Span) bool {
	parentSpanFromRPC := trace.FromContext(sideCtx)
	if parentSpanFromRPC == nil {
		return false
	}

	psc := parentSpanFromRPC.SpanContext()
	span.AddLink(trace.Link{
		SpanID:  psc.SpanID,
		TraceID: psc.TraceID,
		Type:    trace.LinkTypeParent,
	})
	return true
}

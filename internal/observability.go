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
	"time"

	"google.golang.org/grpc"

	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
)

var (
	tagKeyReceiverName, _ = tag.NewKey("opencensus_receiver")
	tagKeyExporterName, _ = tag.NewKey("opencensus_exporter")
	tagKeyError, _        = tag.NewKey("opencensus_error")
	tagKeyStatus, _       = tag.NewKey("opencensus_status")
)

var mReceivedSpans = stats.Int64("oc.io/receiver/received_spans", "Counts the number of spans received by the receiver", "1")

var itemsDistribution = view.Distribution(
	0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 12, 14, 16, 18, 20, 25, 30, 35, 40, 45, 50, 60, 70, 80, 90,
	100, 150, 200, 250, 300, 450, 500, 600, 700, 800, 900, 1000, 1200, 1400, 1600, 1800, 2000,
)

// ViewReceivedSpansReceiver defines the view for the received spans metric.
var ViewReceivedSpansReceiver = &view.View{
	Name:        "oc.io/receiver/received_spans",
	Description: "The number of spans received by the receiver",
	Measure:     mReceivedSpans,
	Aggregation: itemsDistribution,
	TagKeys:     []tag.Key{tagKeyReceiverName},
}

var mExportedSpans = stats.Int64("oc.io/receiver/exported_spans", "Counts the number of exported spans", "1")

// ViewExportedSpans defines the view for exported spans metric.
var ViewExportedSpans = &view.View{
	Name:        "oc.io/receiver/exported_spans",
	Description: "Tracks the number of exported spans",
	Measure:     mExportedSpans,
	Aggregation: itemsDistribution,
	TagKeys:     []tag.Key{tagKeyExporterName},
}

var mLatencyMs = stats.Float64("oc.io/latency", "The latency of the various actions in milliseconds", "ms")

var latencyDistribution = view.Distribution(
	0, 0.01, 0.05, 0.1, 0.3, 0.6, 0.8, 1, 2, 3, 4, 5, 6, 8, 10, 13, 16, 20, 25, 30, 40, 50, 65, 80,
	100, 130, 160, 200, 250, 300, 400, 500, 650, 800, 1000, 2000, 5000, 10000, 20000, 50000, 100000,
)

// VIewLatency defines the view to extract the latencies for various actions.
var ViewLatency = &view.View{
	Name:        mLatencyMs.Name(),
	Description: mLatencyMs.Description(),
	Measure:     mLatencyMs,
	Aggregation: latencyDistribution,
	TagKeys:     []tag.Key{tagKeyReceiverName, tagKeyExporterName},
}

// AllViews has the views for the metrics provided by the agent.
var AllViews = []*view.View{
	ViewReceivedSpansReceiver,
	ViewExportedSpans,
	ViewLatency,
}

// NewReceiverEventRecorder creates a helper function that'll add the name of the
// creating exporter as a tag value in the context that will be used to count the
// the number of spans exported, but it will also create a span to mark the duration
// of activeness of the caller.
func NewReceiverEventRecorder(receiverName string) SpansObservabilityRecorder {
	return &spansRecorder{
		predefinedSpanName: "opencensus.service.receiver." + receiverName + ".Receive",
		startTime:          time.Now(),
		receiverName:       receiverName,
	}
}

// SpansObservabilityRecorder is a helper that is started when
type SpansObservabilityRecorder interface {
	Start(ctx context.Context, ni *commonpb.Node) context.Context
	End(ctx context.Context, nItems int64, err error) context.Context
	UnderlyingSpan() *trace.Span
}

type spansRecorder struct {
	predefinedSpanName string
	span               *trace.Span
	receiverName       string
	exporterName       string
	startTime          time.Time
}

var _ SpansObservabilityRecorder = (*spansRecorder)(nil)

func (sr *spansRecorder) Start(ctx context.Context, ni *commonpb.Node) context.Context {
	sr.startTime = time.Now()
	ctx, sr.span = trace.StartSpan(ctx, sr.predefinedSpanName, trace.WithSampler(trace.NeverSample()))

	if sr.receiverName != "" {
		ctx, _ = tag.New(ctx, tag.Upsert(tagKeyReceiverName, sr.receiverName))
	} else {
		ctx, _ = tag.New(ctx, tag.Upsert(tagKeyExporterName, sr.exporterName))
	}

	return ctx
}

func (sr *spansRecorder) End(ctx context.Context, nItems int64, err error) context.Context {
	var measurements []stats.Measurement
	var tagMutators []tag.Mutator

	if sr.receiverName != "" {
		tagMutators = append(tagMutators, tag.Upsert(tagKeyReceiverName, sr.receiverName))
		measurements = append(measurements, mReceivedSpans.M(nItems))
	} else {
		tagMutators = append(tagMutators, tag.Upsert(tagKeyExporterName, sr.exporterName))
		measurements = append(measurements, mExportedSpans.M(nItems))
	}

	if err == nil {
		tagMutators = append(tagMutators, tag.Upsert(tagKeyStatus, "OK"))
	} else {
		tagMutators = append(tagMutators, tag.Upsert(tagKeyStatus, "ERROR"), tag.Upsert(tagKeyError, err.Error()))
	}

	latencyMs := float64(time.Since(sr.startTime) / time.Millisecond)
	measurements = append(measurements, mLatencyMs.M(latencyMs))
	ctx, _ = tag.New(ctx, tagMutators...)
	stats.Record(ctx, measurements...)
	sr.span.End()

	return ctx
}

func (sr *spansRecorder) UnderlyingSpan() *trace.Span {
	return sr.span
}

// NewExporterRecorder creates a helper function that'll add the name of the
// creating exporter as a tag value in the context that will be used to count the
// the number of spans exported, but it will also create a span to mark the duration
// of activeness of the caller.
func NewExporterEventRecorder(exporterName string) SpansObservabilityRecorder {
	return &spansRecorder{
		predefinedSpanName: "opencensus.service.exporter." + exporterName + ".Export",
		startTime:          time.Now(),
		exporterName:       exporterName,
	}
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

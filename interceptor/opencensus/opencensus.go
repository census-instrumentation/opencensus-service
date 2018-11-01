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

package ocinterceptor

import (
	"context"
	"errors"
	"time"

	"google.golang.org/api/support/bundler"

	"go.opencensus.io/trace"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"github.com/census-instrumentation/opencensus-service/internal"
	"github.com/census-instrumentation/opencensus-service/spanreceiver"
)

// Interceptor is the type used to handle spans from OpenCensus exporters.
type Interceptor struct {
	spanSink         spanreceiver.SpanReceiver
	spanBufferPeriod time.Duration
	spanBufferCount  int
}

// New creates a new opencensus.Interceptor reference.
func New(sr spanreceiver.SpanReceiver, opts ...Option) (*Interceptor, error) {
	if sr == nil {
		return nil, errors.New("needs a non-nil spanReceiver")
	}
	oci := &Interceptor{spanSink: sr}
	for _, opt := range opts {
		opt.WithInterceptor(oci)
	}
	return oci, nil
}

var _ agenttracepb.TraceServiceServer = (*Interceptor)(nil)

var errUnimplemented = errors.New("unimplemented")

// Config handles configuration messages.
func (oci *Interceptor) Config(tcs agenttracepb.TraceService_ConfigServer) error {
	// TODO: Implement when we define the config receiver/sender.
	return errUnimplemented
}

type spansAndNode struct {
	spans []*tracepb.Span
	node  *commonpb.Node
}

var errTraceExportProtocolViolation = errors.New("protocol violation: Export's first message must have a Node")

const interceptorName = "opencensus"

// Export is the gRPC method that receives streamed traces from
// OpenCensus-traceproto compatible libraries/applications.
func (oci *Interceptor) Export(tes agenttracepb.TraceService_ExportServer) error {
	// The bundler will receive batches of spans i.e. []*tracepb.Span
	// We need to ensure that it propagates the interceptor name as a tag
	ctxWithInterceptorName := internal.ContextWithInterceptorName(tes.Context(), interceptorName)
	traceBundler := bundler.NewBundler((*spansAndNode)(nil), func(payload interface{}) {
		oci.batchSpanExporting(ctxWithInterceptorName, payload)
	})

	spanBufferPeriod := oci.spanBufferPeriod
	if spanBufferPeriod <= 0 {
		spanBufferPeriod = 2 * time.Second // Arbitrary value
	}
	spanBufferCount := oci.spanBufferCount
	if spanBufferCount <= 0 {
		// TODO: (@odeke-em) provide an option to disable any buffering
		spanBufferCount = 50 // Arbitrary value
	}

	traceBundler.DelayThreshold = spanBufferPeriod
	traceBundler.BundleCountThreshold = spanBufferCount

	// The first message MUST have a non-nil Node.
	recv, err := tes.Recv()
	if err != nil {
		return err
	}

	// Check the condition that the first message has a non-nil Node.
	if recv.Node == nil {
		return errTraceExportProtocolViolation
	}

	spansMetricsFn := internal.NewReceivedSpansRecorderStreaming(tes.Context(), interceptorName)

	processReceivedSpans := func(ni *commonpb.Node, spans []*tracepb.Span) {
		// Firstly, we'll add them to the bundler.
		if len(spans) > 0 {
			bundlerPayload := &spansAndNode{node: ni, spans: spans}
			traceBundler.Add(bundlerPayload, len(bundlerPayload.spans))
		}

		// We MUST unconditionally record metrics from this reception.
		spansMetricsFn(ni, spans)
	}

	var lastNonNilNode *commonpb.Node
	// Now that we've got the first message with a Node, we can start to receive streamed up spans.
	for {
		// If a Node has been sent from downstream, save and use it.
		if recv.Node != nil {
			lastNonNilNode = recv.Node
		}

		processReceivedSpans(lastNonNilNode, recv.Spans)

		recv, err = tes.Recv()
		if err != nil {
			return err
		}
	}
}

func (oci *Interceptor) batchSpanExporting(longLivedRPCCtx context.Context, payload interface{}) {
	spnL := payload.([]*spansAndNode)
	if len(spnL) == 0 {
		return
	}

	// Trace this method
	ctx, span := trace.StartSpan(context.Background(), "OpenCensusInterceptor.Export")
	defer span.End()

	// TODO: (@odeke-em) investigate if it is necessary
	// to group nodes with their respective spans during
	// spansAndNode list unfurling then send spans grouped per node

	// If the starting RPC has a parent span, then add it as a parent link.
	internal.SetParentLink(longLivedRPCCtx, span)

	nSpans := int64(0)
	for _, spn := range spnL {
		oci.spanSink.ReceiveSpans(ctx, spn.node, spn.spans...)
		nSpans += int64(len(spn.spans))
	}

	span.Annotate([]trace.Attribute{
		trace.Int64Attribute("num_spans", nSpans),
	}, "")
}

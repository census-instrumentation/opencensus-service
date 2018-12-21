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

package octrace

import (
	"context"
	"errors"
	"io"
	"time"

	"google.golang.org/api/support/bundler"

	"go.opencensus.io/trace"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	resourcepb "github.com/census-instrumentation/opencensus-proto/gen-go/resource/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/internal"
	"github.com/census-instrumentation/opencensus-service/receiver"
)

// Receiver is the type used to handle spans from OpenCensus exporters.
type Receiver struct {
	spanSink         receiver.TraceReceiverSink
	spanBufferPeriod time.Duration
	spanBufferCount  int
}

// New creates a new opencensus.Receiver reference.
func New(sr receiver.TraceReceiverSink, opts ...Option) (*Receiver, error) {
	if sr == nil {
		return nil, errors.New("needs a non-nil receiver.TraceReceiverSink")
	}
	oci := &Receiver{spanSink: sr}
	for _, opt := range opts {
		opt.WithReceiver(oci)
	}
	return oci, nil
}

var _ agenttracepb.TraceServiceServer = (*Receiver)(nil)

var errUnimplemented = errors.New("unimplemented")

// Config handles configuration messages.
func (oci *Receiver) Config(tcs agenttracepb.TraceService_ConfigServer) error {
	// TODO: Implement when we define the config receiver/sender.
	return errUnimplemented
}

var errTraceExportProtocolViolation = errors.New("protocol violation: Export's first message must have a Node")

const receiverName = "opencensus_trace"

// Export is the gRPC method that receives streamed traces from
// OpenCensus-traceproto compatible libraries/applications.
func (oci *Receiver) Export(tes agenttracepb.TraceService_ExportServer) error {
	// The bundler will receive batches of spans i.e. []*tracepb.Span
	// We need to ensure that it propagates the receiver name as a tag
	ctxWithReceiverName := internal.ContextWithReceiverName(tes.Context(), receiverName)
	traceBundler := bundler.NewBundler((*data.TraceData)(nil), func(payload interface{}) {
		oci.batchSpanExporting(ctxWithReceiverName, payload)
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

	spansMetricsFn := internal.NewReceivedSpansRecorderStreaming(tes.Context(), receiverName)

	processReceivedSpans := func(ni *commonpb.Node, resource *resourcepb.Resource, spans []*tracepb.Span) {
		// Firstly, we'll add them to the bundler.
		if len(spans) > 0 {
			bundlerPayload := &data.TraceData{Node: ni, Resource: resource, Spans: spans}
			traceBundler.Add(bundlerPayload, len(bundlerPayload.Spans))
		}

		// We MUST unconditionally record metrics from this reception.
		spansMetricsFn(ni, spans)
	}

	var lastNonNilNode *commonpb.Node
	var resource *resourcepb.Resource
	// Now that we've got the first message with a Node, we can start to receive streamed up spans.
	for {
		// If a Node has been sent from downstream, save and use it.
		if recv.Node != nil {
			lastNonNilNode = recv.Node
		}

		// TODO(songya): differentiate between unset and nil resource. See
		// https://github.com/census-instrumentation/opencensus-proto/issues/146.
		if recv.Resource != nil {
			resource = recv.Resource
		}

		processReceivedSpans(lastNonNilNode, resource, recv.Spans)

		recv, err = tes.Recv()
		if err != nil {
			if err == io.EOF {
				// Do not return EOF as an error so that grpc-gateway calls get an empty
				// response with HTTP status code 200 rather than a 500 error with EOF.
				return nil
			}
			return err
		}
	}
}

func (oci *Receiver) batchSpanExporting(longLivedRPCCtx context.Context, payload interface{}) {
	tracedata := payload.([]*data.TraceData)
	if len(tracedata) == 0 {
		return
	}

	// Trace this method
	ctx, span := trace.StartSpan(context.Background(), "OpenCensusTraceReceiver.Export")
	defer span.End()

	// TODO: (@odeke-em) investigate if it is necessary
	// to group nodes with their respective spans during
	// spansAndNode list unfurling then send spans grouped per node

	// If the starting RPC has a parent span, then add it as a parent link.
	internal.SetParentLink(longLivedRPCCtx, span)

	nSpans := int64(0)
	for _, td := range tracedata {
		oci.spanSink.ReceiveTraceData(ctx, *td)
		nSpans += int64(len(td.Spans))
	}

	span.Annotate([]trace.Attribute{
		trace.Int64Attribute("num_spans", nSpans),
	}, "")
}

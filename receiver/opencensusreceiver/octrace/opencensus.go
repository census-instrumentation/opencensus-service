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

	"go.opencensus.io/trace"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	resourcepb "github.com/census-instrumentation/opencensus-proto/gen-go/resource/v1"
	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/internal"
	"github.com/census-instrumentation/opencensus-service/receiver"
)

const (
	defaultNumWorkers = 1
)

// Receiver is the type used to handle spans from OpenCensus exporters.
type Receiver struct {
	spanSink   receiver.TraceReceiverSink
	numWorkers int
}

// New creates a new opencensus.Receiver reference.
func New(sr receiver.TraceReceiverSink, opts ...Option) (*Receiver, error) {
	if sr == nil {
		return nil, errors.New("needs a non-nil receiver.TraceReceiverSink")
	}
	oci := &Receiver{
		spanSink:   sr,
		numWorkers: defaultNumWorkers,
	}
	for _, opt := range opts {
		opt(oci)
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
	// We need to ensure that it propagates the receiver name as a tag
	ctxWithReceiverName := internal.ContextWithReceiverName(tes.Context(), receiverName)

	// The first message MUST have a non-nil Node.
	recv, err := tes.Recv()
	if err != nil {
		return err
	}

	messageChan := make(chan *data.TraceData, 64)
	workers := make([]*receiverWorker, 0, oci.numWorkers)
	for index := 0; index < oci.numWorkers; index++ {
		worker := newReceiverWorker(ctxWithReceiverName, oci, tes)
		go worker.listenOn(messageChan)
		workers = append(workers)
	}

	defer func() {
		for _, worker := range workers {
			worker.stopListening()
		}
	}()

	// Check the condition that the first message has a non-nil Node.
	if recv.Node == nil {
		return errTraceExportProtocolViolation
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

		td := &data.TraceData{
			Node:     lastNonNilNode,
			Resource: resource,
			Spans:    recv.Spans,
		}

		messageChan <- td

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

type receiverWorker struct {
	receiver     *Receiver
	tes          agenttracepb.TraceService_ExportServer
	longLivedCtx context.Context
	cancel       chan struct{}
}

func newReceiverWorker(
	ctx context.Context, receiver *Receiver, tes agenttracepb.TraceService_ExportServer,
) *receiverWorker {
	return &receiverWorker{
		receiver:     receiver,
		tes:          tes,
		longLivedCtx: ctx,
		cancel:       make(chan struct{}),
	}
}

func (rw *receiverWorker) listenOn(cn chan *data.TraceData) {
	for {
		select {
		case td := <-cn:
			rw.export(td)
		case <-rw.cancel:
			return
		}
	}
}

func (rw *receiverWorker) stopListening() {
	close(rw.cancel)
}

func (rw *receiverWorker) export(tracedata *data.TraceData) {
	if tracedata == nil {
		return
	}

	// We MUST unconditionally record metrics from this reception.
	spansMetricsFn := internal.NewReceivedSpansRecorderStreaming(rw.tes.Context(), receiverName)
	spansMetricsFn(tracedata.Node, tracedata.Spans)

	if len(tracedata.Spans) == 0 {
		return
	}

	// Trace this method
	ctx, span := trace.StartSpan(context.Background(), "OpenCensusTraceReceiver.Export")
	defer span.End()

	// TODO: (@odeke-em) investigate if it is necessary
	// to group nodes with their respective spans during
	// spansAndNode list unfurling then send spans grouped per node

	// If the starting RPC has a parent span, then add it as a parent link.
	internal.SetParentLink(rw.longLivedCtx, span)

	rw.receiver.spanSink.ReceiveTraceData(ctx, *tracedata)

	span.Annotate([]trace.Attribute{
		trace.Int64Attribute("num_spans", int64(len(tracedata.Spans))),
	}, "")
}

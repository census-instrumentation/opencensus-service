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

package ocmetrics

import (
	"context"
	"errors"
	"time"

	"google.golang.org/api/support/bundler"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agentmetricspb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/metrics/v1"
	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
	resourcepb "github.com/census-instrumentation/opencensus-proto/gen-go/resource/v1"
	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/internal"
	"github.com/census-instrumentation/opencensus-service/receiver"
)

// Receiver is the type used to handle metrics from OpenCensus exporters.
type Receiver struct {
	metricSink         receiver.MetricsReceiverSink
	metricBufferPeriod time.Duration
	metricBufferCount  int
}

// New creates a new ocmetrics.Receiver reference.
func New(sr receiver.MetricsReceiverSink, opts ...Option) (*Receiver, error) {
	if sr == nil {
		return nil, errors.New("needs a non-nil receiver.MetricsReceiverSink")
	}
	ocr := &Receiver{metricSink: sr}
	for _, opt := range opts {
		opt.WithReceiver(ocr)
	}
	return ocr, nil
}

var _ agentmetricspb.MetricsServiceServer = (*Receiver)(nil)

var errMetricsExportProtocolViolation = errors.New("protocol violation: Export's first message must have a Node")

const receiverName = "opencensus_metrics"

// Export is the gRPC method that receives streamed metrics from
// OpenCensus-metricproto compatible libraries/applications.
func (ocr *Receiver) Export(mes agentmetricspb.MetricsService_ExportServer) (err error) {
	var nReceivedMessages int64

	// The bundler will receive batches of metrics i.e. []*metricspb.Metric
	// We need to ensure that it propagates the receiver name as a tag
	observabilityRecorder := internal.NewReceiverEventRecorder(receiverName)
	ctx := observabilityRecorder.Start(mes.Context(), nil)
	defer observabilityRecorder.End(ctx, nReceivedMessages, err)

	metricsBundler := bundler.NewBundler((*data.MetricsData)(nil), func(payload interface{}) {
		ocr.batchMetricExporting(ctx, payload)
	})

	metricBufferPeriod := ocr.metricBufferPeriod
	if metricBufferPeriod <= 0 {
		metricBufferPeriod = 2 * time.Second // Arbitrary value
	}
	metricBufferCount := ocr.metricBufferCount
	if metricBufferCount <= 0 {
		// TODO: (@odeke-em) provide an option to disable any buffering
		metricBufferCount = 50 // Arbitrary value
	}

	metricsBundler.DelayThreshold = metricBufferPeriod
	metricsBundler.BundleCountThreshold = metricBufferCount

	// Retrieve the first message. It MUST have a non-nil Node.
	recv, err := mes.Recv()
	if err != nil {
		return err
	}

	// Check the condition that the first message has a non-nil Node.
	if recv.Node == nil {
		return errMetricsExportProtocolViolation
	}

	var lastNonNilNode *commonpb.Node
	var resource *resourcepb.Resource
	// Now that we've got the first message with a Node, we can start to receive streamed up metrics.
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

		processReceivedMetrics(lastNonNilNode, resource, recv.Metrics, metricsBundler)

		recv, err = mes.Recv()
		if err != nil {
			return err
		}

		// Otherwise mark this as a received messages.
		nReceivedMessages++
	}
}

func processReceivedMetrics(ni *commonpb.Node, resource *resourcepb.Resource, metrics []*metricspb.Metric, bundler *bundler.Bundler) {
	// Firstly, we'll add them to the bundler.
	if len(metrics) > 0 {
		bundlerPayload := &data.MetricsData{Node: ni, Metrics: metrics, Resource: resource}
		bundler.Add(bundlerPayload, len(bundlerPayload.Metrics))
	}
}

func (ocr *Receiver) batchMetricExporting(longLivedRPCCtx context.Context, payload interface{}) {
	mds := payload.([]*data.MetricsData)
	if len(mds) == 0 {
		return
	}

	// TODO: (@odeke-em) investigate if it is necessary
	// to group nodes with their respective metrics during
	// bundledMetrics list unfurling then send metrics grouped per node

	for _, md := range mds {
		metricsRecorder := internal.NewReceiverEventRecorder(receiverName)
                ctx := metricsRecorder.Start(longLivedRPCCtx, md.Node)
		// If the starting RPC has a parent span, then add it as a parent link.
		internal.SetParentLink(longLivedRPCCtx, metricsRecorder.UnderlyingSpan())

		ack, err := ocr.metricSink.ReceiveMetricsData(ctx, *md)
		var nItems int64
		if ack != nil {
			nItems = int64(ack.SavedMetrics)
		}
		metricsRecorder.End(ctx, nItems, err)
	}
}

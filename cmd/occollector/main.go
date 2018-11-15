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

// Program occollector receives stats and traces from multiple sources and
// batches them for appropriate forwarding to backends (e.g.: Jaeger or Zipkin)
// or other layers of occollector. The forwarding can be configured so
// buffer sizes, number of retries, backoff policy, etc can be ajusted according
// to specific needs of each deployment.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
	resourcepb "github.com/census-instrumentation/opencensus-proto/gen-go/resource/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"github.com/census-instrumentation/opencensus-service/metricsink"
	"github.com/census-instrumentation/opencensus-service/receiver/opencensus"
	"github.com/census-instrumentation/opencensus-service/spansink"

	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats/view"
	"go.uber.org/zap"
)

const (
	defaultOCAddress = ":55678"
)

func main() {
	var signalsChannel = make(chan os.Signal)
	signal.Notify(signalsChannel, os.Interrupt, syscall.SIGTERM)

	// TODO: Allow configuration of logger and other items such as servers, etc.
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to get production logger: %v", err)
	}

	logger.Info("Starting...")

	closeSrv, err := runOCServerWithReceiver(defaultOCAddress, logger)
	if err != nil {
		logger.Fatal("Cannot run opencensus server", zap.String("Address", defaultOCAddress), zap.Error(err))
	}

	logger.Info("Collector is up and running.")

	<-signalsChannel
	logger.Info("Starting shutdown...")

	// TODO: orderly shutdown: first receivers, then flushing pipelines giving
	// senders a chance to send all their data. This may take time, the allowed
	// time should be part of configuration.
	closeSrv()

	logger.Info("Shutdown complete.")
}

func runOCServerWithReceiver(addr string, logger *zap.Logger) (func() error, error) {
	if err := view.Register(ocgrpc.DefaultServerViews...); err != nil {
		return nil, fmt.Errorf("Failed to register ocgrpc.DefaultServerViews: %v", err)
	}

	ss := &fakeSpanSink{
		logger: logger,
	}

	ms := &fakeMetricSink{
		logger: logger,
	}

	ocr, err := opencensus.New(addr)
	if err != nil {
		return nil, fmt.Errorf("Failed to create the OpenCensus receiver: %v", err)
	}
	if err := ocr.StartTraceReception(context.Background(), ss); err != nil {
		return nil, fmt.Errorf("Failed to start OpenCensus TraceReceiver : %v", err)
	}
	if err := ocr.StartMetricsReception(context.Background(), ms); err != nil {
		return nil, fmt.Errorf("Failed to start OpenCensus MetricsReceiver : %v", err)
	}
	return ocr.Stop, nil
}

type fakeSpanSink struct {
	logger *zap.Logger
}

func (sr *fakeSpanSink) ReceiveSpans(ctx context.Context, node *commonpb.Node, spans ...*tracepb.Span) (*spansink.Acknowledgement, error) {
	ack := &spansink.Acknowledgement{
		SavedSpans: uint64(len(spans)),
	}

	sr.logger.Info("ReceivedSpans", zap.Uint64("Received spans", ack.SavedSpans))

	return ack, nil
}

type fakeMetricSink struct {
	logger *zap.Logger
}

func (mr *fakeMetricSink) ReceiveMetrics(ctx context.Context, node *commonpb.Node, resource *resourcepb.Resource, metrics ...*metricspb.Metric) (*metricsink.Acknowledgement, error) {
	ack := &metricsink.Acknowledgement{
		SavedMetrics: uint64(len(metrics)),
	}

	mr.logger.Info("ReceivedMetrics", zap.Uint64("Received metrics", ack.SavedMetrics))

	return ack, nil
}

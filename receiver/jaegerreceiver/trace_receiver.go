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

package jaegerreceiver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	agentapp "github.com/jaegertracing/jaeger/cmd/agent/app"
	"github.com/jaegertracing/jaeger/cmd/agent/app/configmanager"
	"github.com/jaegertracing/jaeger/cmd/agent/app/reporter"
	"github.com/jaegertracing/jaeger/cmd/collector/app"
	"github.com/jaegertracing/jaeger/model"
	jaegerproto "github.com/jaegertracing/jaeger/proto-gen/api_v2"
	"github.com/jaegertracing/jaeger/thrift-gen/baggage"
	"github.com/jaegertracing/jaeger/thrift-gen/jaeger"
	"github.com/jaegertracing/jaeger/thrift-gen/sampling"
	"github.com/jaegertracing/jaeger/thrift-gen/zipkincore"
	"github.com/uber/jaeger-lib/metrics"
	tchannel "github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/thrift"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/census-instrumentation/opencensus-service/consumer"
	"github.com/census-instrumentation/opencensus-service/observability"
	"github.com/census-instrumentation/opencensus-service/receiver"
	jaegertranslator "github.com/census-instrumentation/opencensus-service/translator/trace/jaeger"
)

// Configuration defines the behavior and the ports that
// the Jaeger receiver will use.
type Configuration struct {
	CollectorThriftPort int `yaml:"tchannel_port"`
	CollectorHTTPPort   int `yaml:"collector_http_port"`
	CollectorGRPCPort   int `yaml:"collector_grpc_port"`

	AgentPort              int `mapstructure:"agent_port"`
	AgentCompactThriftPort int `mapstructure:"agent_compact_thrift_port"`
	AgentBinaryThriftPort  int `mapstructure:"agent_binary_thrift_port"`
}

// Receiver type is used to receive spans that were originally intended to be sent to Jaeger.
// This receiver is basically a Jaeger collector.
type jReceiver struct {
	// mu protects the fields of this type
	mu sync.Mutex

	nextConsumer consumer.TraceConsumer

	startOnce sync.Once
	stopOnce  sync.Once

	config *Configuration
	logger *zap.Logger

	agent       *agentapp.Agent
	agentServer *http.Server

	tchannel            *tchannel.Channel
	collectorServer     *http.Server
	collectorGRPCServer *grpc.Server

	defaultAgentCtx context.Context
}

const (
	// As per https://www.jaegertracing.io/docs/1.7/deployment/
	// By default, the port used by jaeger-agent to send spans in jaeger.thrift format
	defaultTChannelPort = 14267
	// By default, can accept spans directly from clients in jaeger.thrift format over binary thrift protocol
	defaultCollectorHTTPPort = 14268
	// By default, can accept spans directly from clients in protobuf format over gRPC
	defaultGRPCPort = 14250

	// As per https://www.jaegertracing.io/docs/1.7/deployment/#agent
	// 5775	UDP accept zipkin.thrift over compact thrift protocol
	// 6831	UDP accept jaeger.thrift over compact thrift protocol
	// 6832	UDP accept jaeger.thrift over binary thrift protocol
	defaultZipkinThriftUDPPort  = 5775
	defaultCompactThriftUDPPort = 6831
	defaultBinaryThriftUDPPort  = 6832

	traceSource      string = "Jaeger"
	defaultAgentPort        = 5778
)

// New creates a TraceReceiver that receives traffic as a collector with both Thrift and HTTP transports.
func New(ctx context.Context, logger *zap.Logger, config *Configuration, nextConsumer consumer.TraceConsumer) (receiver.TraceReceiver, error) {
	return &jReceiver{
		config:          config,
		defaultAgentCtx: observability.ContextWithReceiverName(context.Background(), "jaeger-agent"),
		logger:          logger,
		nextConsumer:    nextConsumer,
	}, nil
}

var _ receiver.TraceReceiver = (*jReceiver)(nil)

var (
	errAlreadyStarted = errors.New("already started")
	errAlreadyStopped = errors.New("already stopped")
)

func (jr *jReceiver) collectorAddr() string {
	var port int
	if jr.config != nil {
		port = jr.config.CollectorHTTPPort
	}
	if port <= 0 {
		port = defaultCollectorHTTPPort
	}
	return fmt.Sprintf(":%d", port)
}

func (jr *jReceiver) collectorGRPCAddr() string {
	var port int
	if jr.config != nil {
		port = jr.config.CollectorGRPCPort
	}
	if port <= 0 {
		port = defaultGRPCPort
	}
	return fmt.Sprintf(":%d", port)
}

func (jr *jReceiver) agentAddress() string {
	var port int
	if jr.config != nil {
		port = jr.config.AgentPort
	}
	if port <= 0 {
		port = defaultAgentPort
	}
	return fmt.Sprintf(":%d", port)
}

func (jr *jReceiver) tchannelAddr() string {
	var port int
	if jr.config != nil {
		port = jr.config.CollectorThriftPort
	}
	if port <= 0 {
		port = defaultTChannelPort
	}
	return fmt.Sprintf(":%d", port)
}

func (jr *jReceiver) agentCompactThriftAddr() string {
	var port int
	if jr.config != nil {
		port = jr.config.AgentCompactThriftPort
	}
	if port <= 0 {
		port = defaultCompactThriftUDPPort
	}
	return fmt.Sprintf(":%d", port)
}

func (jr *jReceiver) agentBinaryThriftAddr() string {
	var port int
	if jr.config != nil {
		port = jr.config.AgentBinaryThriftPort
	}
	if port <= 0 {
		port = defaultBinaryThriftUDPPort
	}
	return fmt.Sprintf(":%d", port)
}

func (jr *jReceiver) TraceSource() string {
	return traceSource
}

func (jr *jReceiver) StartTraceReception(ctx context.Context, asyncErrorChan chan<- error) error {
	jr.mu.Lock()
	defer jr.mu.Unlock()

	var err = errAlreadyStarted
	jr.startOnce.Do(func() {
		if err = jr.startAgent(); err != nil && err != errAlreadyStarted {
			jr.stopTraceReceptionLocked(context.Background())
			return
		}

		if err = jr.startCollector(); err != nil && err != errAlreadyStarted {
			jr.stopTraceReceptionLocked(context.Background())
			return
		}

		err = nil
	})
	return err
}

func (jr *jReceiver) StopTraceReception(ctx context.Context) error {
	jr.mu.Lock()
	defer jr.mu.Unlock()

	return jr.stopTraceReceptionLocked(ctx)
}

func (jr *jReceiver) stopTraceReceptionLocked(ctx context.Context) error {
	var err = errAlreadyStopped
	jr.stopOnce.Do(func() {
		var errs []error

		if jr.agent != nil {
			jr.agent.Stop()
			jr.agent = nil
		}

		if jr.collectorServer != nil {
			if cerr := jr.collectorServer.Close(); cerr != nil {
				errs = append(errs, cerr)
			}
			jr.collectorServer = nil
		}
		if jr.tchannel != nil {
			jr.tchannel.Close()
			jr.tchannel = nil
		}
		if jr.collectorGRPCServer != nil {
			jr.collectorGRPCServer.GracefulStop()
		}
		if len(errs) == 0 {
			err = nil
			return
		}
		// Otherwise combine all these errors
		buf := new(bytes.Buffer)
		for _, err := range errs {
			fmt.Fprintf(buf, "%s\n", err.Error())
		}
		err = errors.New(buf.String())
	})

	return err
}

const collectorReceiverTagValue = "jaeger-collector"

// SubmitBatches implments jaeger's jaeger/cmd/collector/app/span_handler.go JaegerBatchesHandler
// interface so that jReceiver can be used as a JaegerBatchesHandler for the tchannel handler
func (jr *jReceiver) SubmitBatches(ctx thrift.Context, batches []*jaeger.Batch) ([]*jaeger.BatchSubmitResponse, error) {
	jbsr := make([]*jaeger.BatchSubmitResponse, 0, len(batches))
	ctxWithReceiverName := observability.ContextWithReceiverName(ctx, collectorReceiverTagValue)

	for _, batch := range batches {
		td, err := jaegertranslator.ThriftBatchToOCProto(batch)
		// TODO: (@odeke-em) add this error for Jaeger observability
		ok := false

		if err == nil {
			ok = true
			td.SourceFormat = "jaeger"
			jr.nextConsumer.ConsumeTraceData(ctx, td)
			// We MUST unconditionally record metrics from this reception.
			observability.RecordTraceReceiverMetrics(ctxWithReceiverName, len(batch.Spans), len(batch.Spans)-len(td.Spans))
		}

		jbsr = append(jbsr, &jaeger.BatchSubmitResponse{
			Ok: ok,
		})
	}
	return jbsr, nil
}

// ProcessSpans implemenents jaeger's jaeger/cmd/collector/app/span_handler.go SpanProcessor
// interface so that the jReceiver can be used as a GRPC span processor.
func (jr *jReceiver) ProcessSpans(mSpans []*model.Span, spanFormat string) ([]bool, error) {
	response := make([]bool, 0, len(mSpans))
	ocTraces, err := tracetranslator.JaegerProtoToOCProtoBatch(mSpans)

	// If we failed to convert all the spans, return false for all spans received.
	if err != nil {
		for range mSpans {
			response = append(response, false)
		}
		return response, nil
	}

	for _, ocTrace := range ocTraces {
		if ocTrace != nil {
			ctx := context.Background()
			_, err = jr.spanSink.ReceiveTraceData(ctx, data.TraceData{Node: ocTrace.Node, Spans: ocTrace.Spans})
		}

		// Return true for all the spans in the batch if err is nil, otherwise return false
		for range ocTrace.Spans {
			response = append(response, err == nil)
		}
	}

	return response, nil
}

var _ reporter.Reporter = (*jReceiver)(nil)
var _ agentapp.CollectorProxy = (*jReceiver)(nil)

// EmitZipkinBatch implements cmd/agent/reporter.Reporter and it forwards
// Zipkin spans received by the Jaeger agent processor.
func (jr *jReceiver) EmitZipkinBatch(spans []*zipkincore.Span) error {
	return nil
}

// EmitBatch implements cmd/agent/reporter.Reporter and it forwards
// Jaeger spans received by the Jaeger agent processor.
func (jr *jReceiver) EmitBatch(batch *jaeger.Batch) error {
	td, err := jaegertranslator.ThriftBatchToOCProto(batch)
	if err != nil {
		observability.RecordTraceReceiverMetrics(jr.defaultAgentCtx, len(batch.Spans), len(batch.Spans))
		return err
	}

	err = jr.nextConsumer.ConsumeTraceData(jr.defaultAgentCtx, td)
	observability.RecordTraceReceiverMetrics(jr.defaultAgentCtx, len(batch.Spans), len(batch.Spans)-len(td.Spans))

	return err
}

func (jr *jReceiver) GetReporter() reporter.Reporter {
	return jr
}

func (jr *jReceiver) GetManager() configmanager.ClientConfigManager {
	return jr
}

func (jr *jReceiver) GetSamplingStrategy(serviceName string) (*sampling.SamplingStrategyResponse, error) {
	return &sampling.SamplingStrategyResponse{}, nil
}

func (jr *jReceiver) GetBaggageRestrictions(serviceName string) ([]*baggage.BaggageRestriction, error) {
	return nil, nil
}

func (jr *jReceiver) startAgent() error {
	processorConfigs := []agentapp.ProcessorConfiguration{
		{
			// Compact Thrift running by default on 6831.
			Model:    "jaeger",
			Protocol: "compact",
			Server: agentapp.ServerConfiguration{
				HostPort: jr.agentCompactThriftAddr(),
			},
		},
		{
			// Binary Thrift running by default on 6832.
			Model:    "jaeger",
			Protocol: "binary",
			Server: agentapp.ServerConfiguration{
				HostPort: jr.agentBinaryThriftAddr(),
			},
		},
	}

	builder := agentapp.Builder{
		Processors: processorConfigs,
		HTTPServer: agentapp.HTTPServerConfiguration{
			HostPort: jr.agentAddress(),
		},
	}

	agent, err := builder.CreateAgent(jr, zap.NewNop(), metrics.NullFactory)
	if err != nil {
		return err
	}

	if err := agent.Run(); err != nil {
		return err
	}

	// Otherwise no error was encountered,
	jr.agent = agent

	return nil
}

// startCollector starts a thrift server, http server, and gRPC server to accept
// spans in any jaeger-supported format
func (jr *jReceiver) startCollector() error {
	err := jr.startThriftServer()
	if err != nil {
		return err
	}
	err = jr.startHTTPServer()
	if err != nil {
		return err
	}

	err = jr.startGRPCServer()
	if err != nil {
		return err
	}

	return nil
}

func (jr *jReceiver) startThriftServer() error {
	tch, terr := tchannel.NewChannel("jaeger-collector", new(tchannel.ChannelOptions))
	if terr != nil {
		return fmt.Errorf("Failed to create NewTChannel: %v", terr)
	}

	server := thrift.NewServer(tch)
	server.Register(jaeger.NewTChanCollectorServer(jr))

	addr := jr.tchannelAddr()
	ln, terr := net.Listen("tcp", addr)
	if terr != nil {
		return fmt.Errorf("Failed to bind to TChannnel address %q: %v", addr, terr)
	}
	tch.Serve(ln)
	jr.tchannel = tch
	return nil
}

func (jr *jReceiver) startHTTPServer() error {
	addr := jr.collectorAddr()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		ln.Close()
		return fmt.Errorf("Failed to bind to Collector address %q: %v", addr, err)
	}

	nr := mux.NewRouter()
	apiHandler := app.NewAPIHandler(jr)
	apiHandler.RegisterRoutes(nr)
	jr.collectorServer = &http.Server{Handler: nr}
	go func() {
		_ = jr.collectorServer.Serve(ln)
	}()
	return nil
}

func (jr *jReceiver) startGRPCServer() error {
	server := grpc.NewServer()
	addr := jr.collectorGRPCAddr()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		ln.Close()
		return fmt.Errorf("Failed to bind to grpc Collector address %q: %v", addr, err)
	}
	jaegerproto.RegisterCollectorServiceServer(server, app.NewGRPCHandler(jr.logger, jr))
	jr.collectorGRPCServer = server

	jr.logger.Info("Starting jaeger grpc server", zap.String("addr", addr))
	go func() {
		if err := server.Serve(ln); err != nil {
			jr.logger.Fatal("gRPC collector failed", zap.Error(err))
		}
	}()

	return err
}

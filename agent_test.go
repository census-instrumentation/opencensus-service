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

package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/timestamp"
	"google.golang.org/grpc"

	"go.opencensus.io/trace"
	"go.opencensus.io/trace/tracestate"

	"contrib.go.opencensus.io/exporter/ocagent"
	"github.com/census-instrumentation/opencensus-agent"
	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
)

func TestAgent(t *testing.T) {
	theAgent, evr, port, doneFn := runningAgent(t)
	defer doneFn()

	agentExporter, err := ocagent.NewExporter(ocagent.WithPort(uint16(port)), ocagent.WithInsecure())
	if err != nil {
		t.Fatalf("Failed to create the agent exporter: %v", err)
	}
	trace.RegisterExporter(agentExporter)
	defer func() {
		agentExporter.Stop()
		trace.UnregisterExporter(agentExporter)
	}()

	// Firstly push configs down to each connected node
	evr.forEachConfigNode(func(configNode *commonpb.Node) {
		theAgent.SendConfig(configNode, &agenttracepb.UpdatedLibraryConfig{
			Config: &tracepb.TraceConfig{Sampler: &tracepb.TraceConfig_ConstantSampler{ConstantSampler: &tracepb.ConstantSampler{Decision: true}}},
		})
	})

	ctx, span := trace.StartSpan(context.Background(), "TestAgentSpan", trace.WithSampler(trace.AlwaysSample()))
	nSpans := 100
	for i := 0; i < nSpans; i++ {
		_, childSpan := trace.StartSpan(ctx, fmt.Sprintf("ChildSpan-%d", i))
		<-time.After(5 * time.Millisecond)
		childSpan.End()
	}
	span.End()
	<-time.After(50 * time.Millisecond)

	agentExporter.Flush()

	// Give it time for the exporter to flush and upload the spans.
	<-time.After(150 * time.Millisecond)

	spanCount := uint64(0)
	evr.forEachUploadedSpan(func(span *tracepb.Span) {
		if span != nil {
			spanCount += 1
		} else {
			t.Error("Unexpected got a nil span")
		}
	})

	if g, w := spanCount, 1+uint64(nSpans); g != w {
		t.Errorf("SpanCount: got %d want %d", g, w)
	}
}

func TestAgent_integrityOfExportedSpans(t *testing.T) {
	_, evr, port, doneFn := runningAgent(t)
	defer doneFn()

	agentExporter, err := ocagent.NewExporter(ocagent.WithPort(uint16(port)), ocagent.WithInsecure())
	if err != nil {
		t.Fatalf("Failed to create the agent exporter: %v", err)
	}
	trace.RegisterExporter(agentExporter)
	defer func() {
		agentExporter.Stop()
		trace.UnregisterExporter(agentExporter)
	}()

	now := time.Now().UTC()
	clientSpanData := &trace.SpanData{
		StartTime: now.Add(-10 * time.Second),
		EndTime:   now.Add(20 * time.Second),
		SpanContext: trace.SpanContext{
			TraceID:      trace.TraceID{0x4F, 0x4E, 0x4D, 0x4C, 0x4B, 0x4A, 0x49, 0x48, 0x47, 0x46, 0x45, 0x44, 0x43, 0x42, 0x41},
			SpanID:       trace.SpanID{0x7F, 0x7E, 0x7D, 0x7C, 0x7B, 0x7A, 0x79, 0x78},
			TraceOptions: trace.TraceOptions(0x01),
		},
		ParentSpanID: trace.SpanID{0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37},
		Name:         "ClientSpan",
		Status:       trace.Status{Code: trace.StatusCodeInternal, Message: "Blocked by firewall"},
		SpanKind:     trace.SpanKindClient,
	}

	serverSpanData := &trace.SpanData{
		StartTime: now.Add(-5 * time.Second),
		EndTime:   now.Add(10 * time.Second),
		SpanContext: trace.SpanContext{
			TraceID:      trace.TraceID{0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2A, 0x2B, 0x2C, 0x2D, 0x2E},
			SpanID:       trace.SpanID{0xF0, 0xF1, 0xF2, 0xF3, 0xF4, 0xF5, 0xF6, 0xF7},
			TraceOptions: trace.TraceOptions(0x01),
			Tracestate:   &tracestate.Tracestate{},
		},
		ParentSpanID: trace.SpanID{0x38, 0x39, 0x3A, 0x3B, 0x3C, 0x3D, 0x3E, 0x3F},
		Name:         "ServerSpan",
		Status:       trace.Status{Code: trace.StatusCodeOK, Message: "OK"},
		SpanKind:     trace.SpanKindServer,
		Links: []trace.Link{
			{
				TraceID: trace.TraceID{0x4F, 0x4E, 0x4D, 0x4C, 0x4B, 0x4A, 0x49, 0x48, 0x47, 0x46, 0x45, 0x44, 0x43, 0x42, 0x41, 0x40},
				SpanID:  trace.SpanID{0x7F, 0x7E, 0x7D, 0x7C, 0x7B, 0x7A, 0x79, 0x78},
				Type:    trace.LinkTypeParent,
			},
		},
	}

	agentExporter.ExportSpan(serverSpanData)
	agentExporter.ExportSpan(clientSpanData)

	agentExporter.Flush()

	// Give it time for the exporter to flush and upload the spans.
	<-time.After(150 * time.Millisecond)

	// Now span inspection and verification time!
	wantSpans := []*tracepb.Span{
		{
			TraceId:      serverSpanData.TraceID[:],
			SpanId:       serverSpanData.SpanID[:],
			ParentSpanId: serverSpanData.ParentSpanID[:],
			Name:         &tracepb.TruncatableString{Value: "ServerSpan"},
			Kind:         tracepb.Span_SERVER,
			StartTime:    timeToTimestamp(serverSpanData.StartTime),
			EndTime:      timeToTimestamp(serverSpanData.EndTime),
			Status:       &tracepb.Status{Code: int32(serverSpanData.Status.Code), Message: serverSpanData.Status.Message},
			Tracestate:   &tracepb.Span_Tracestate{},
			Links: &tracepb.Span_Links{
				Link: []*tracepb.Span_Link{
					{
						TraceId: []byte{0x4F, 0x4E, 0x4D, 0x4C, 0x4B, 0x4A, 0x49, 0x48, 0x47, 0x46, 0x45, 0x44, 0x43, 0x42, 0x41, 0x40},
						SpanId:  []byte{0x7F, 0x7E, 0x7D, 0x7C, 0x7B, 0x7A, 0x79, 0x78},
						Type:    tracepb.Span_Link_PARENT_LINKED_SPAN,
					},
				},
			},
		},
		{
			TraceId:      clientSpanData.TraceID[:],
			SpanId:       clientSpanData.SpanID[:],
			ParentSpanId: clientSpanData.ParentSpanID[:],
			Name:         &tracepb.TruncatableString{Value: "ClientSpan"},
			Kind:         tracepb.Span_CLIENT,
			StartTime:    timeToTimestamp(clientSpanData.StartTime),
			EndTime:      timeToTimestamp(clientSpanData.EndTime),
			Status:       &tracepb.Status{Code: int32(clientSpanData.Status.Code), Message: clientSpanData.Status.Message},
		},
	}

	var gotSpans []*tracepb.Span
	evr.forEachUploadedSpan(func(span *tracepb.Span) {
		if span != nil {
			gotSpans = append(gotSpans, span)
		} else {
			t.Error("Unexpected got a nil span")
		}
	})

	if g, w := len(gotSpans), len(wantSpans); g != w {
		t.Errorf("SpanCount: got %d want %d", g, w)
	}

	if !reflect.DeepEqual(gotSpans, wantSpans) {
		gotBlob, _ := json.MarshalIndent(gotSpans, "", "  ")
		wantBlob, _ := json.MarshalIndent(wantSpans, "", "  ")
		t.Errorf("GotSpans:\n%s\nWantSpans:\n%s", gotBlob, wantBlob)
	}
}

func TestAgent_updatedLibraryConfigResponses(t *testing.T) {
	theAgent, evr, port, doneFn := runningAgent(t)
	defer doneFn()

	// All this client does is echo back to the agent the configs
	// that are beamed down to it, just like the ocagent will
	// except that we don't apply the global config here.
	addr := fmt.Sprintf("localhost:%d", port)
	cc, err := grpc.Dial(addr, grpc.WithBlock(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Failed to create gRPC connection to agent: %v", err)
	}
	defer cc.Close()

	traceSvcClient := agenttracepb.NewTraceServiceClient(cc)
	configStream, err := traceSvcClient.Config(context.Background())
	if err != nil {
		t.Fatalf("Failed to create configStream: %v", err)
	}

	// Firstly conform to the protocol by the client sending up the identifier information
	starterConfig := &agenttracepb.CurrentLibraryConfig{
		Node: &commonpb.Node{Identifier: &commonpb.ProcessIdentifier{Pid: uint32(os.Getpid() + 99)}},
	}
	if err := configStream.Send(starterConfig); err != nil {
		t.Fatalf("Failed to send the initiating config message: %v", err)
	}

	// Give it some time to simmer and make the "handshake".
	<-time.After(40 * time.Millisecond)

	// Now the config stream is ready to start
	// echo-ing back the configs that it receives.
	go func() {
		for {
			recv, err := configStream.Recv()
			if err != nil {
				return
			}
			err = configStream.Send(&agenttracepb.CurrentLibraryConfig{
				Config: &tracepb.TraceConfig{Sampler: recv.Config.Sampler},
			})
			if err != nil {
				return
			}
		}
	}()

	configsToSendDown := []*agenttracepb.UpdatedLibraryConfig{
		{
			Config: &tracepb.TraceConfig{Sampler: &tracepb.TraceConfig_ConstantSampler{ConstantSampler: &tracepb.ConstantSampler{Decision: true}}},
		},
		{
			Config: &tracepb.TraceConfig{Sampler: &tracepb.TraceConfig_ConstantSampler{ConstantSampler: &tracepb.ConstantSampler{Decision: false}}},
		},
		{
			Config: &tracepb.TraceConfig{Sampler: &tracepb.TraceConfig_ProbabilitySampler{ProbabilitySampler: &tracepb.ProbabilitySampler{SamplingProbability: 1.0}}},
		},
		{
			Config: &tracepb.TraceConfig{Sampler: &tracepb.TraceConfig_ProbabilitySampler{ProbabilitySampler: &tracepb.ProbabilitySampler{SamplingProbability: 0.0}}},
		},
		{
			Config: &tracepb.TraceConfig{Sampler: &tracepb.TraceConfig_ProbabilitySampler{ProbabilitySampler: &tracepb.ProbabilitySampler{SamplingProbability: 0.33333}}},
		},
		{
			Config: &tracepb.TraceConfig{Sampler: &tracepb.TraceConfig_RateLimitingSampler{RateLimitingSampler: &tracepb.RateLimitingSampler{Qps: 1e9}}},
		},
	}

	// Firstly push configs down to each connected node
	for _, config := range configsToSendDown {
		theAgent.SendConfig(starterConfig.Node, config)
	}
	<-time.After(100 * time.Millisecond)

	var gotConfigs []*agenttracepb.CurrentLibraryConfig
	evr.forEachReceivedConfig(func(cfg *agenttracepb.CurrentLibraryConfig) {
		if cfg != nil {
			gotConfigs = append(gotConfigs, cfg)
		} else {
			t.Error("Unexpected got a nil currentLibraryConfig")
		}
	})

	// We expect to have all these configs to have been beamed
	// back to the agent in the exact order and integrity.
	wantConfigs := []*agenttracepb.CurrentLibraryConfig{
		{
			Config: &tracepb.TraceConfig{Sampler: &tracepb.TraceConfig_ConstantSampler{ConstantSampler: &tracepb.ConstantSampler{Decision: true}}},
		},
		{
			Config: &tracepb.TraceConfig{Sampler: &tracepb.TraceConfig_ConstantSampler{ConstantSampler: &tracepb.ConstantSampler{Decision: false}}},
		},
		{
			Config: &tracepb.TraceConfig{Sampler: &tracepb.TraceConfig_ProbabilitySampler{ProbabilitySampler: &tracepb.ProbabilitySampler{SamplingProbability: 1.0}}},
		},
		{
			Config: &tracepb.TraceConfig{Sampler: &tracepb.TraceConfig_ProbabilitySampler{ProbabilitySampler: &tracepb.ProbabilitySampler{SamplingProbability: 0.0}}},
		},
		{
			Config: &tracepb.TraceConfig{Sampler: &tracepb.TraceConfig_ProbabilitySampler{ProbabilitySampler: &tracepb.ProbabilitySampler{SamplingProbability: 0.33333}}},
		},
		{
			Config: &tracepb.TraceConfig{Sampler: &tracepb.TraceConfig_RateLimitingSampler{RateLimitingSampler: &tracepb.RateLimitingSampler{Qps: 1e9}}},
		},
	}
	if !reflect.DeepEqual(gotConfigs, wantConfigs) {
		gotBlob, _ := json.MarshalIndent(gotConfigs, "", "  ")
		wantBlob, _ := json.MarshalIndent(wantConfigs, "", "  ")
		t.Errorf("GotConfigs:\n%s\nWantConfigs:\n%s", gotBlob, wantBlob)
	}
}

func runningAgent(t *testing.T) (theAgent *agent.Agent, evr *eventsRecorder, port int, done func()) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to find an available address to run the gRPC server: %v", err)
	}

	doneFnList := []func(){func() { ln.Close() }}
	done = func() {
		for _, doneFn := range doneFnList {
			doneFn()
		}
	}

	_, port, err = hostPortFromAddr(ln.Addr())
	if err != nil {
		done()
		t.Fatalf("Failed to parse host:port from listener address: %s error: %v", ln.Addr(), err)
	}

	evr = &eventsRecorder{t: t}
	theAgent, err = agent.NewAgent(
		agent.WithOnTracesReceivedFunc(evr.onTracesReceived),
		agent.WithOnLibraryConfigReceivedFunc(evr.onCurrentLibraryConfig),
		agent.WithOnConfigNodeAcceptedFunc(evr.onConfigNodeAccepted))

	if err != nil {
		done()
		t.Fatalf("Failed to create new agent: %v", err)
	}

	// Now run it as a gRPC server
	srv := grpc.NewServer()
	agenttracepb.RegisterTraceServiceServer(srv, theAgent)
	go func() {
		_ = srv.Serve(ln)
	}()

	return theAgent, evr, port, done
}

func hostPortFromAddr(addr net.Addr) (host string, port int, err error) {
	addrStr := addr.String()
	sepIndex := strings.LastIndex(addrStr, ":")
	if sepIndex < 0 {
		return "", -1, errors.New("failed to parse host:port")
	}
	host, portStr := addrStr[:sepIndex], addrStr[sepIndex+1:]
	port, err = strconv.Atoi(portStr)
	return host, port, err
}

type eventsRecorder struct {
	sync.RWMutex
	t                      *testing.T
	tracesFromUploads      []*tracepb.Span
	configNodes            []*commonpb.Node
	receivedLibraryConfigs []*agenttracepb.CurrentLibraryConfig
}

func (evr *eventsRecorder) onCurrentLibraryConfig(node *commonpb.Node, timeAt time.Time, res *agenttracepb.CurrentLibraryConfig, err error) error {
	if res != nil {
		evr.Lock()
		evr.receivedLibraryConfigs = append(evr.receivedLibraryConfigs, res)
		evr.Unlock()
	}

	return err
}

func (evr *eventsRecorder) onTracesReceived(node *commonpb.Node, timeAt time.Time, req *agenttracepb.ExportTraceServiceRequest, err error) error {
	switch {
	case req == nil && err == nil:
		evr.t.Error("Unexpectedly got a nil ExportTraceServiceRequest")
	case err != nil:
		// TODO: Decide how to handle this error
	default:
		evr.Lock()
		evr.tracesFromUploads = append(evr.tracesFromUploads, req.Spans...)
		evr.Unlock()
	}

	return err
}

func (evr *eventsRecorder) onConfigNodeAccepted(configNode *commonpb.Node, timeAt time.Time) {
	evr.Lock()
	evr.configNodes = append(evr.configNodes, configNode)
	evr.Unlock()
}

func (evr *eventsRecorder) forEachConfigNode(fn func(*commonpb.Node)) {
	evr.RLock()
	defer evr.RUnlock()

	for _, configNode := range evr.configNodes {
		fn(configNode)
	}
}

func (evr *eventsRecorder) forEachUploadedSpan(fn func(*tracepb.Span)) {
	evr.RLock()
	defer evr.RUnlock()

	for _, span := range evr.tracesFromUploads {
		fn(span)
	}
}

func (evr *eventsRecorder) forEachReceivedConfig(fn func(*agenttracepb.CurrentLibraryConfig)) {
	evr.RLock()
	defer evr.RUnlock()

	for _, cfg := range evr.receivedLibraryConfigs {
		fn(cfg)
	}
}

func timeToTimestamp(t time.Time) *timestamp.Timestamp {
	nanoTime := t.UnixNano()
	return &timestamp.Timestamp{
		Seconds: nanoTime / 1e9,
		Nanos:   int32(nanoTime % 1e9),
	}
}

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

// Program ocagent collects OpenCensus stats and traces
// to export to a configured backend.
package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"

	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/zpages"

	"github.com/census-instrumentation/opencensus-service/exporter"
	"github.com/census-instrumentation/opencensus-service/internal"
	"github.com/census-instrumentation/opencensus-service/metricsink"
	"github.com/census-instrumentation/opencensus-service/receiver/jaeger"
	"github.com/census-instrumentation/opencensus-service/receiver/opencensus"
	"github.com/census-instrumentation/opencensus-service/receiver/zipkin"
	"github.com/census-instrumentation/opencensus-service/spansink"
)

var configYAMLFile string
var ocReceiverPort int

const zipkinRoute = "/api/v2/spans"

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func runOCAgent() {
	yamlBlob, err := ioutil.ReadFile(configYAMLFile)
	if err != nil {
		log.Fatalf("Cannot read the YAML file %v error: %v", configYAMLFile, err)
	}
	agentConfig, err := parseOCAgentConfig(yamlBlob)
	if err != nil {
		log.Fatalf("Failed to parse own configuration %v error: %v", configYAMLFile, err)
	}

	// Ensure that we check and catch any logical errors with the
	// configuration e.g. if an receiver shares the same address
	// as an exporter which would cause a self DOS and waste resources.
	if err := agentConfig.checkLogicalConflicts(yamlBlob); err != nil {
		log.Fatalf("Configuration logical error: %v", err)
	}

	ocReceiverAddr := agentConfig.ocReceiverAddress()

	traceExporters, closeFns := exportersFromYAMLConfig(yamlBlob)
	commonSpanSink := exporter.MultiTraceExporters(traceExporters...)

	// TODO: create metricsSink after we add metric exporter interface.

	// Add other receivers here as they are implemented
	ocReceiverDoneFn, err := runOCReceiver(ocReceiverAddr, commonSpanSink, nil)
	if err != nil {
		log.Fatal(err)
	}
	closeFns = append(closeFns, ocReceiverDoneFn)

	// If zPages are enabled, run them
	zPagesPort, zPagesEnabled := agentConfig.zPagesPort()
	if zPagesEnabled {
		zCloseFn := runZPages(zPagesPort)
		closeFns = append(closeFns, zCloseFn)
	}

	// If the Zipkin receiver is enabled, then run it
	if agentConfig.zipkinReceiverEnabled() {
		zipkinReceiverAddr := agentConfig.zipkinReceiverAddress()
		zipkinReceiverDoneFn, err := runZipkinReceiver(zipkinReceiverAddr, commonSpanSink)
		if err != nil {
			log.Fatal(err)
		}
		closeFns = append(closeFns, zipkinReceiverDoneFn)
	}

	if agentConfig.jaegerReceiverEnabled() {
		collectorHTTPPort, collectorThriftPort := agentConfig.jaegerReceiverPorts()
		jaegerDoneFn, err := runJaegerReceiver(collectorThriftPort, collectorHTTPPort, commonSpanSink)
		if err != nil {
			log.Fatal(err)
		}
		closeFns = append(closeFns, jaegerDoneFn)
	}

	// Always cleanup finally
	defer func() {
		for _, closeFn := range closeFns {
			if closeFn != nil {
				closeFn()
			}
		}
	}()

	signalsChan := make(chan os.Signal)
	signal.Notify(signalsChan, os.Interrupt)

	// Wait for the closing signal
	<-signalsChan
}

func runZPages(port int) func() error {
	// And enable zPages too
	zPagesMux := http.NewServeMux()
	zpages.Handle(zPagesMux, "/debug")

	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to bind to run zPages on %q: %v", addr, err)
	}

	srv := http.Server{Handler: zPagesMux}
	go func() {
		log.Printf("Running zPages at %q", addr)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to serve zPages: %v", err)
		}
	}()

	return srv.Close
}

func runOCReceiver(addr string, sr spansink.Sink, mr metricsink.Sink) (doneFn func() error, err error) {
	ocr, err := opencensus.New(addr)
	if err != nil {
		return nil, fmt.Errorf("Failed to create the OpenCensus receiver on address %q: error %v", addr, err)
	}
	if err := view.Register(internal.AllViews...); err != nil {
		return nil, fmt.Errorf("Failed to register internal.AllViews: %v", err)
	}
	if err := view.Register(ocgrpc.DefaultServerViews...); err != nil {
		return nil, fmt.Errorf("Failed to register ocgrpc.DefaultServerViews: %v", err)
	}

	if err := ocr.StartTraceReception(context.Background(), sr); err != nil {
		return nil, fmt.Errorf("Failed to start TraceReceiver: %v", err)
	}
	log.Printf("Running OpenCensus Trace receiver as a gRPC service at %q", addr)

	if mr != nil {
		if err := ocr.StartMetricsReception(context.Background(), mr); err != nil {
			return nil, fmt.Errorf("Failed to start MetricsReceiver: %v", err)
		}
		log.Printf("Running OpenCensus Metrics receiver as a gRPC service at %q", addr)
	}

	doneFn = ocr.Stop
	return doneFn, nil
}

func runJaegerReceiver(collectorThriftPort, collectorHTTPPort int, sr spansink.Sink) (doneFn func() error, err error) {
	jtr, err := jaeger.New(context.Background(), collectorThriftPort, collectorHTTPPort)
	if err != nil {
		return nil, fmt.Errorf("Failed to create new Jaeger receiver: %v", err)
	}
	if err := jtr.StartTraceReception(context.Background(), sr); err != nil {
		return nil, fmt.Errorf("Failed to start Jaeger receiver: %v", err)
	}
	doneFn = func() error {
		return jtr.StopTraceReception(context.Background())
	}
	log.Printf("Running Jaeger receiver with CollectorThriftPort %d CollectHTTPPort %d", collectorThriftPort, collectorHTTPPort)
	return doneFn, nil
}

func runZipkinReceiver(addr string, sr spansink.Sink) (doneFn func() error, err error) {
	zi, err := zipkinreceiver.New(sr)
	if err != nil {
		return nil, fmt.Errorf("Failed to create the Zipkin receiver: %v", err)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("Cannot bind Zipkin receiver to address %q: %v", addr, err)
	}
	mux := http.NewServeMux()
	mux.Handle(zipkinRoute, zi)
	go func() {
		fullAddr := addr + zipkinRoute
		log.Printf("Running the Zipkin receiver at %q", fullAddr)
		if err := http.Serve(ln, mux); err != nil {
			log.Fatalf("Failed to serve the Zipkin receiver: %v", err)
		}
	}()

	doneFn = ln.Close
	return doneFn, nil
}

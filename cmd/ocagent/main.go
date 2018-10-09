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
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"time"

	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	"github.com/census-instrumentation/opencensus-service/cmd/ocagent/exporterparser"
	"github.com/census-instrumentation/opencensus-service/cmd/ocagent/plugins"
	"github.com/census-instrumentation/opencensus-service/exporter"
	"github.com/census-instrumentation/opencensus-service/interceptor/opencensus"
	"github.com/census-instrumentation/opencensus-service/internal"
	"github.com/census-instrumentation/opencensus-service/spanreceiver"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats/view"
)

func main() {
	ocInterceptorPort := flag.Int("oci-port", 55678, "The port on which the OpenCensus interceptor is run")
	exportersYAMLConfigFile := flag.String("exporters-yaml", "config.yaml", "The YAML file with the configurations for the various exporters")
	pluginPaths := flag.String("plugins", "", "A comma separated string specifying the paths of the various exporter plugins")

	flag.Parse()

	yamlBlob, err := ioutil.ReadFile(*exportersYAMLConfigFile)
	if err != nil {
		log.Fatalf("Cannot read the YAML file %v error: %v", exportersYAMLConfigFile, err)
	}
	traceExporters, _, closeFns := exporterparser.ExportersFromYAMLConfig(yamlBlob)
	// Always cleanup finally
	defer func() {
		for _, closeFn := range closeFns {
			if closeFn != nil {
				closeFn()
			}
		}
	}()
	inCodeExportersSpanReceiver := exporter.OCExportersToTraceExporter(traceExporters...)

	// Load the various traceExporter plugins if specified
	pluginsSpanReceiver, pluginsDoneFn := plugins.LoadTraceExporterPlugins(yamlBlob, *pluginPaths)
	defer pluginsDoneFn()

	// Combine the various spanreceiver.SpanReceiver instances into one
	commonSpanReceiver := spanreceiver.Multi(inCodeExportersSpanReceiver, pluginsSpanReceiver)

	// Add other interceptors here as they are implemented
	ocInterceptorDoneFn, err := runOCInterceptor(*ocInterceptorPort, commonSpanReceiver)
	if err != nil {
		log.Fatal(err)
	}
	closeFns = append(closeFns, ocInterceptorDoneFn)

	signalsChan := make(chan os.Signal)
	signal.Notify(signalsChan, os.Interrupt)

	// Wait for the closing signal
	<-signalsChan
}

func runOCInterceptor(ocInterceptorPort int, sr spanreceiver.SpanReceiver) (doneFn func(), err error) {
	oci, err := ocinterceptor.New(sr, ocinterceptor.WithSpanBufferPeriod(800*time.Millisecond))
	if err != nil {
		return nil, fmt.Errorf("Failed to create the OpenCensus interceptor: %v", err)
	}

	addr := fmt.Sprintf("localhost:%d", ocInterceptorPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("Cannot bind to address %q: %v", addr, err)
	}
	srv := internal.GRPCServerWithObservabilityEnabled()
	if err := view.Register(internal.AllViews...); err != nil {
		return nil, fmt.Errorf("Failed to register internal.AllViews: %v", err)
	}
	if err := view.Register(ocgrpc.DefaultServerViews...); err != nil {
		return nil, fmt.Errorf("Failed to register ocgrpc.DefaultServerViews: %v", err)
	}

	agenttracepb.RegisterTraceServiceServer(srv, oci)
	go func() {
		log.Printf("Running OpenCensus interceptor as a gRPC service at %q", addr)
		_ = srv.Serve(ln)
	}()
	doneFn = func() { _ = ln.Close() }
	return doneFn, nil
}

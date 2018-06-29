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

package exporter

import (
	"log"

	openzipkin "github.com/openzipkin/zipkin-go"
	"github.com/openzipkin/zipkin-go/reporter/http"
	"go.opencensus.io/exporter/zipkin"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	yaml "gopkg.in/yaml.v2"
)

type zipkinConfig struct {
	Zipkin struct {
		Endpoint string `yaml:"endpoint,omitempty"`
	} `yaml:"zipkin,omitempty"`
}

type zipkinExporter struct{}

func (z *zipkinExporter) MakeExporters(config []byte) (se view.Exporter, te trace.Exporter, closer func()) {
	var c zipkinConfig
	if err := yaml.Unmarshal(config, &c); err != nil {
		log.Fatalf("Cannot unmarshal data: %v", err)
	}
	if endpoint := c.Zipkin.Endpoint; endpoint != "" {
		// TODO(jbd): Propagate service name, hostport and more metadata from each node.
		localEndpoint, err := openzipkin.NewEndpoint("", "")
		if err != nil {
			log.Fatalf("Cannot configure Zipkin exporter: %v", err)
		}
		reporter := http.NewReporter(endpoint)
		te = zipkin.NewExporter(reporter, localEndpoint)
		closer = func() {
			if err := reporter.Close(); err != nil {
				log.Printf("Cannot close the Zipkin reporter: %v\n", err)
			}
		}
	}
	return se, te, closer
}

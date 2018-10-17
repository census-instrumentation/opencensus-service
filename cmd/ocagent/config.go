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

package main

import (
	"log"

	yaml "gopkg.in/yaml.v2"

	"github.com/census-instrumentation/opencensus-service/exporter"
	"github.com/census-instrumentation/opencensus-service/exporter/exporterparser"
)

// We expect the configuration.yaml file to look like this:
//
//  interceptors:
//      opencensus:
//          port: <port>
//      zipkin:
//          reporter: <address>
//
//  exporters:
//      stackdriver:
//          project: <project_id>
//          enable_tracing: true
//      zipkin:
//          endpoint: "http://localhost:9411/api/v2/spans"
//
//  zpages:
//      port: 55679

const (
	defaultOCInterceptorAddress     = "localhost:55678"
	defaultZPagesPort               = 55679
	defaultZipkinInterceptorAddress = "localhost:9411"
)

type config struct {
	Interceptors *interceptors `yaml:"interceptors"`
	ZPages       *zPagesConfig `yaml:"zpages"`
}

type interceptors struct {
	OpenCensus *interceptorConfig `yaml:"opencensus"`
	Zipkin     *interceptorConfig `yaml:"zipkin"`
}

type interceptorConfig struct {
	// The address to which the OpenCensus interceptor will be bound and run on.
	Address string `yaml:"address"`
}

type zPagesConfig struct {
	Disabled bool `yaml:"disabled"`
	Port     int  `yaml:"port"`
}

func (c *config) ocInterceptorAddress() string {
	if c == nil || c.Interceptors == nil {
		return defaultOCInterceptorAddress
	}
	inCfg := c.Interceptors
	if inCfg.OpenCensus == nil || inCfg.OpenCensus.Address == "" {
		return defaultOCInterceptorAddress
	}
	return inCfg.OpenCensus.Address
}

func (c *config) zPagesDisabled() bool {
	if c == nil {
		return true
	}
	return c.ZPages != nil && c.ZPages.Disabled
}

func (c *config) zPagesPort() (int, bool) {
	if c.zPagesDisabled() {
		return -1, false
	}
	port := defaultZPagesPort
	if c != nil && c.ZPages != nil && c.ZPages.Port > 0 {
		port = c.ZPages.Port
	}
	return port, true
}

func (c *config) zipkinInterceptorEnabled() bool {
	if c == nil {
		return false
	}
	return c.Interceptors != nil && c.Interceptors.Zipkin != nil
}

func (c *config) zipkinInterceptorAddress() string {
	if c == nil || c.Interceptors == nil {
		return defaultZipkinInterceptorAddress
	}
	inCfg := c.Interceptors
	if inCfg.Zipkin == nil {
		return defaultZipkinInterceptorAddress
	}
	return inCfg.Zipkin.Address
}

func parseOCAgentConfig(yamlBlob []byte) (*config, error) {
	var cfg config
	if err := yaml.Unmarshal(yamlBlob, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// exportersFromYAMLConfig parses the config yaml payload and returns the respective exporters
func exportersFromYAMLConfig(config []byte) (traceExporters []exporter.TraceExporter, doneFns []func() error) {
	parseFns := []struct {
		name string
		fn   func([]byte) ([]exporter.TraceExporter, []func() error, error)
	}{
		{name: "datadog", fn: exporterparser.DatadogTraceExportersFromYAML},
		{name: "stackdriver", fn: exporterparser.StackdriverTraceExportersFromYAML},
		{name: "zipkin", fn: exporterparser.ZipkinExportersFromYAML},
		{name: "jaeger", fn: exporterparser.JaegerExportersFromYAML},
	}

	for _, cfg := range parseFns {
		tes, tesDoneFns, err := cfg.fn(config)
		if err != nil {
			log.Fatalf("Failed to create config for %q: %v", cfg.name, err)
		}
		nonNilExporters := 0
		for _, te := range tes {
			if te != nil {
				traceExporters = append(traceExporters, te)
				nonNilExporters += 1
			}
		}

		// TODO: (@odeke-em) Add a catch here prevent logical errors
		// e.g. if the Zipkin interceptor port is the exact
		// same as that of the exporter, DO NOT run, crash hard
		// lest we'll just recursively send traffic back to selves
		// which is a potential self-DOS.

		if nonNilExporters > 0 {
			pluralization := "exporter"
			if nonNilExporters > 1 {
				pluralization = "exporters"
			}
			log.Printf("%q trace-%s enabled", cfg.name, pluralization)
		}
		for _, doneFn := range tesDoneFns {
			doneFns = append(doneFns, doneFn)
		}
	}
	return
}

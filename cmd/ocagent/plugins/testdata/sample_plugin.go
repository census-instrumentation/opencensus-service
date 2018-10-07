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

/*
This file serves as a canonical example of writing a plugin
for TraceExporter. It is consumed in plugins_test.go
*/
package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.opencensus.io/trace"
	yaml "gopkg.in/yaml.v2"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	"github.com/census-instrumentation/opencensus-service/exporter"
)

type eachExporter struct {
	mu       sync.Mutex
	stopped  bool
	spandata []*trace.SpanData
}

func NewTraceExporterPlugin() exporter.TraceExporterPlugin {
	return new(parser)
}

type parser struct {
	es []*eachExporter
}

type config struct {
	Counter *struct {
		Count int `yaml:"count"`
	} `yaml:"counter"`
}

func (p *parser) TraceExportersFromConfig(blob []byte, configType exporter.ConfigType) (eachExporters []exporter.TraceExporter, err error) {
	if configType != exporter.ConfigTypeYAML {
		return nil, fmt.Errorf("Expected ConfigTypeYAML instead of %v", configType)
	}
	cfg := new(config)
	if err := yaml.Unmarshal(blob, cfg); err != nil {
		return nil, err
	}
	cnt := cfg.Counter
	if cnt == nil {
		return nil, errors.New("Failed to parse a counter")
	}

	for i := 0; i < cnt.Count; i++ {
		ex := new(eachExporter)
		eachExporters = append(eachExporters, ex)
		p.es = append(p.es, ex)
	}
	return
}

func (p *parser) Stop(ctx context.Context) error {
	for _, e := range p.es {
		e.Stop(ctx)
	}
	return nil
}

func (e *eachExporter) ExportSpanData(node *commonpb.Node, spandata ...*trace.SpanData) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.stopped {
		return fmt.Errorf("Already stopped")
	}
	e.spandata = append(e.spandata, spandata...)
	return nil
}

func (e *eachExporter) Stop(ctx context.Context) {
	e.mu.Lock()
	if !e.stopped {
		e.stopped = true
	}
	e.mu.Unlock()
}

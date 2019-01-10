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

package exporterparser

import (
	"context"
	"fmt"

	"contrib.go.opencensus.io/exporter/stackdriver"

	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/exporter"
)

type stackdriverConfig struct {
	ProjectID     string `yaml:"project,omitempty"`
	EnableTracing bool   `yaml:"enable_tracing,omitempty"`
	EnableMetrics bool   `yaml:"enable_metrics,omitempty"`
}

type stackdriverExporter struct {
	exporter *stackdriver.Exporter
}

var _ exporter.TraceExporter = (*stackdriverExporter)(nil)

// StackdriverTraceExportersFromYAML parses the yaml bytes and returns an exporter.TraceExporter targeting
// Stackdriver according to the configuration settings.
func StackdriverTraceExportersFromYAML(config []byte) (tes []exporter.TraceExporter, mes []exporter.MetricsExporter, doneFns []func() error, err error) {
	var cfg struct {
		Exporters *struct {
			Stackdriver *stackdriverConfig `yaml:"stackdriver"`
		} `yaml:"exporters"`
	}
	if err := yamlUnmarshal(config, &cfg); err != nil {
		return nil, nil, nil, err
	}
	if cfg.Exporters == nil {
		return nil, nil, nil, nil
	}
	sc := cfg.Exporters.Stackdriver
	if sc == nil {
		return nil, nil, nil, nil
	}
	if !sc.EnableTracing && !sc.EnableMetrics {
		return nil, nil, nil, nil
	}

	// TODO:  For each ProjectID, create a different exporter
	// or at least a unique Stackdriver client per ProjectID.
	if sc.ProjectID == "" {
		return nil, nil, nil, fmt.Errorf("Stackdriver config requires a project ID")
	}

	sde, serr := stackdriver.NewExporter(stackdriver.Options{
		ProjectID: sc.ProjectID,
	})
	if serr != nil {
		return nil, nil, nil, fmt.Errorf("Cannot configure Stackdriver Trace exporter: %v", serr)
	}

	stexp := &stackdriverExporter{exporter: sde}
	if sc.EnableTracing {
		tes = append(tes, stexp)
	}
	if sc.EnableMetrics {
		// TODO: (@odeke-em, @songya23) implement ExportMetrics for Stackdriver.
		// mes = append(mes, oexp)
	}
	doneFns = append(doneFns, func() error {
		sde.Flush()
		return nil
	})
	return tes, mes, doneFns, nil
}

func (sde *stackdriverExporter) ExportSpans(ctx context.Context, td data.TraceData) error {
	// TODO: Examine "contrib.go.opencensus.io/exporter/stackdriver" to see
	// if trace.ExportSpan was constraining and if perhaps the Stackdriver
	// upload can use the context and information from the Node.
	return exportSpans(ctx, "stackdriver", sde.exporter, td)
}

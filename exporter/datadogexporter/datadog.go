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

package datadogexporter

import (
	datadog "github.com/DataDog/opencensus-go-exporter-datadog"
	"github.com/spf13/viper"

	"github.com/census-instrumentation/opencensus-service/consumer"
	"github.com/census-instrumentation/opencensus-service/exporter/exporterwrapper"
)

type datadogConfig struct {
	// ServiceName specifies the service name used for tracing.
	ServiceName string `mapstructure:"service_name,omitempty"`

	// Namespace specifies the namespaces to which metric keys are appended.
	Namespace string `mapstructure:"namespace,omitempty"`

	// TraceAddr specifies the host[:port] address of the Datadog Trace Agent.
	// It defaults to localhost:8126.
	TraceAddr string `mapstructure:"trace_addr,omitempty"`

	// MetricsAddr specifies the host[:port] address for DogStatsD. It defaults
	// to localhost:8125.
	MetricsAddr string `mapstructure:"metrics_addr,omitempty"`

	// Tags specifies a set of global tags to attach to each metric.
	Tags []string `mapstructure:"tags,omitempty"`

	EnableTracing bool `mapstructure:"enable_tracing,omitempty"`
	EnableMetrics bool `mapstructure:"enable_metrics,omitempty"`
}

// DatadogTraceExportersFromViper unmarshals the viper and returns an exporter.TraceExporter targeting
// Datadog according to the configuration settings.
func DatadogTraceExportersFromViper(v *viper.Viper) (tps []consumer.TraceConsumer, mps []consumer.MetricsConsumer, doneFns []func() error, err error) {
	var cfg struct {
		Datadog *datadogConfig `mapstructure:"datadog,omitempty"`
	}
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, nil, nil, err
	}

	dc := cfg.Datadog
	if dc == nil {
		return nil, nil, nil, nil
	}
	if !dc.EnableTracing && !dc.EnableMetrics {
		return nil, nil, nil, nil
	}

	// TODO(jbd): Create new exporter for each service name.
	de := datadog.NewExporter(datadog.Options{
		Service:   dc.ServiceName,
		Namespace: dc.Namespace,
		TraceAddr: dc.TraceAddr,
		StatsAddr: dc.MetricsAddr,
		Tags:      dc.Tags,
	})
	doneFns = append(doneFns, func() error {
		de.Stop()
		return nil
	})

	dgte, err := exporterwrapper.NewExporterWrapper("datadog", "ocservice.exporter.DataDog.ConsumeTraceData", de)
	if err != nil {
		return nil, nil, nil, err
	}

	// TODO: Examine the Datadog exporter to see
	// if trace.ExportSpan was constraining and if perhaps the
	// upload can use the context and information from the Node.
	tps = append(tps, dgte)

	// TODO: (@odeke-em, @songya23) implement ExportMetrics for Datadog.
	// mes = append(mes, oexp)

	return
}

// Copyright 2019, OpenCensus Authors
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
	"github.com/spf13/viper"

	"github.com/census-instrumentation/opencensus-service/processor"
)

// TraceExporter composes TraceDataProcessor with some additional
// exporter-specific functions. This helps the service core to identify which
// TraceDataProcessors are Exporters and which are internal processing
// components, so that better validation of pipelines can be done.
type TraceExporter interface {
	processor.TraceDataProcessor

	// TraceExportFormat gets the name of the format in which this exporter sends its data.
	// For exporters that can export multiple signals it is recommended to encode the signal
	// as suffix (e.g. "oc_trace").
	TraceExportFormat() string
}

// TraceExporterFactory is an interface that builds a new TraceExporter based on
// some viper.Viper configuration.
type TraceExporterFactory interface {
	// Type gets the type of the TraceExporter created by this factory.
	Type() string
	// NewFromViper takes a viper.Viper config and creates a new TraceExporter.
	NewFromViper(cfg *viper.Viper) (TraceExporter, error)
	// DefaultConfig returns the default configuration for TraceExporter
	// created by this factory.
	DefaultConfig() *viper.Viper
}

// MetricsExporter composes MetricsDataProcessor with some additional
// exporter-specific functions. This helps the service core to identify which
// MetricsDataProcessors are Exporters and which are internal processing
// components, so that better validation of pipelines can be done.
type MetricsExporter interface {
	processor.MetricsDataProcessor

	// MetricsExportFormat gets the name of the format in which this exporter sends its data.
	// For exporters that can export multiple signals it is recommended to encode the signal
	// as suffix (e.g. "oc_metrics").
	MetricsExportFormat() string
}

// MetricsExporterFactory is an interface that builds a new MetricsExporter based on
// some viper.Viper configuration.
type MetricsExporterFactory interface {
	// Type gets the type of the MetricsExporter created by this factory.
	Type() string
	// NewFromViper takes a viper.Viper config and creates a new MetricsExporter.
	NewFromViper(cfg *viper.Viper) (MetricsExporter, error)
	// DefaultConfig returns the default configuration for MetricsExporter
	// created by this factory.
	DefaultConfig() *viper.Viper
}

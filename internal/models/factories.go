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

package models

import (
	"fmt"

	"github.com/census-instrumentation/opencensus-service/exporter"
	"github.com/census-instrumentation/opencensus-service/processor"
	"github.com/census-instrumentation/opencensus-service/receiver"
)

// Note: logically factories do not belong to models package and must be moved to
// receiver,exporter,options packages. However doing so will require extensive changes
// to multiple source code files. I plan to do it in a separate commit when I start
// implementing specific factories and when I will have to touch that part of
// the code anyway.

///////////////////////////////////////////////////////////////////////////////
// Receiver factory and its registry

// ReceiverFactory is factory interface for receivers.
type ReceiverFactory interface {
	// Type gets the type of the Receiver created by this factory.
	Type() string

	// CreateDefaultConfig creates the default configuration for the Receiver.
	CreateDefaultConfig() ReceiverCfg

	// CreateTraceReceiver creates a trace receiver based on this config.
	// If the receiver type does not support tracing or if the config is not valid
	// error will be returned instead.
	CreateTraceReceiver(cfg ReceiverCfg) (*receiver.TraceReceiver, error)

	// CreateMetricsReceiver creates a metrics receiver based on this config.
	// If the receiver type does not support metrics or if the config is not valid
	// error will be returned instead.
	CreateMetricsReceiver(cfg ReceiverCfg) (*receiver.MetricsReceiver, error)
}

// List of registered receiver factories.
var receiverFactories = make(map[string]ReceiverFactory)

// RegisterReceiverFactory registers a receiver factory.
func RegisterReceiverFactory(factory ReceiverFactory) error {
	if receiverFactories[factory.Type()] != nil {
		panic(fmt.Sprintf("duplicate receiver factory %q", factory.Type()))
	}

	receiverFactories[factory.Type()] = factory
	return nil
}

// GetReceiverFactory gets a receiver factory by type string.
func GetReceiverFactory(typeStr string) ReceiverFactory {
	return receiverFactories[typeStr]
}

///////////////////////////////////////////////////////////////////////////////
// Exporter factory and its registry

// ExporterFactory is is factory interface for exporters
type ExporterFactory interface {
	// Type gets the type of the Exporter created by this factory.
	Type() string

	// CreateDefaultConfig creates the default configuration for the Exporter.
	CreateDefaultConfig() ExporterCfg

	// CreateTraceExporter creates a trace exporter based on this config.
	// If the exporter type does not support trace or if the config is not valid
	// error will be returned instead.
	CreateTraceExporter(cfg ExporterCfg) (*exporter.TraceExporter, error)

	// CreateMetricsExporter creates a metrics exporter based on this config.
	// If the exporter type does not support metrics or if the config is not valid
	// error will be returned instead.
	CreateMetricsExporter(cfg ExporterCfg) (*exporter.MetricsExporter, error)
}

// List of registered exporter factories.
var exporterFactories = make(map[string]ExporterFactory)

// RegisterExporterFactory registers a exporter factory
func RegisterExporterFactory(factory ExporterFactory) error {
	if exporterFactories[factory.Type()] != nil {
		panic(fmt.Sprintf("duplicate exporter factory %q", factory.Type()))
	}

	exporterFactories[factory.Type()] = factory
	return nil
}

// GetExporterFactory gets a exporter factory by type string
func GetExporterFactory(typeStr string) ExporterFactory {
	return exporterFactories[typeStr]
}

///////////////////////////////////////////////////////////////////////////////
// Option factory and its registry

// OptionFactory is is factory interface for options.
type OptionFactory interface {
	// Type gets the type of the Option created by this factory.
	Type() string

	// CreateDefaultConfig creates the default configuration for the Option.
	CreateDefaultConfig() OptionCfg

	// CreateTraceProcessor creates a trace processor based on this config.
	// If the option type does not support trace or if the config is not valid
	// error will be returned instead.
	CreateTraceProcessor(cfg OptionCfg) (*processor.TraceProcessor, error)

	// CreateMetricsProcessor creates a metrics processor based on this config.
	// If the option type does not support metrics or if the config is not valid
	// error will be returned instead.
	CreateMetricsProcessor(cfg OptionCfg) (*processor.MetricsProcessor, error)
}

// List of registered option factories.
var optionFactories = make(map[string]OptionFactory)

// RegisterOptionFactory registers a option factory.
func RegisterOptionFactory(factory OptionFactory) error {
	if optionFactories[factory.Type()] != nil {
		panic(fmt.Sprintf("duplicate option factory %q", factory.Type()))
	}

	optionFactories[factory.Type()] = factory
	return nil
}

// GetOptionFactory gets a option factory by type string.
func GetOptionFactory(typeStr string) OptionFactory {
	return optionFactories[typeStr]
}

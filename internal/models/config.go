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

// Package models defines the data models for entities. This file defines the
// models for V2 configuration format. The defined entities are:
// ConfigV2 (the top-level structure), Receivers, Exporters, Options, Pipelines.
package models

/*
Receivers, Exporters and Options typically have common configuration settings, however
sometimes specific implementations (e.g. StackDriver) will have extra configuration settings.
This requires the configuration data for these entities to be polymorphic.

To satisfy these requirements we declare interfaces ReceiverCfg, ExporterCfg, OptionCfg,
which define the behavior. We also provide helper structs ReceiverCommon, ExporterCommon,
OptionCommon, which define the common settings and un-marshaling from config files.

Specific Receivers/Exporters/Options are expected to at the minimum implement the
corresponding interface and if they have additional settings they must also extend
the corresponding common settings struct (the easiest approach is to embed the common struct).

For example usage see tests.
*/

// ConfigV2 defines the configuration V2 for the various elements of collector or agent.
type ConfigV2 struct {
	Receivers []ReceiverCfg `mapstructure:"-"`
	Exporters []ExporterCfg `mapstructure:"-"`
	Options   []OptionCfg   `mapstructure:"-"`
	Pipelines Pipelines     `mapstructure:"pipelines"`
}

// ReceiversKeyName is the configuration key name for receivers section.
const ReceiversKeyName = "receivers"

// ExportersKeyName is the configuration key name for exporters section.
const ExportersKeyName = "exporters"

// OptionsKeyName is the configuration key name for options section.
const OptionsKeyName = "options"

// Pipelines defines the pipelines section of configuration.
type Pipelines struct {
	Traces  []*Pipeline `mapstructure:"traces"`
	Metrics []*Pipeline `mapstructure:"metrics"`
}

// Pipeline defines a single pipeline.
type Pipeline struct {
	Name       string              `mapstructure:"name"`
	Receivers  []PipelineReceiver  `mapstructure:"receivers"`
	Operations []PipelineOperation `mapstructure:"ordered-operations"`
	Exporters  []PipelineExporter  `mapstructure:"exporters"`
}

// PipelineReceiver defines a receiver of a pipeline.
type PipelineReceiver struct {
	Name string `mapstructure:"name"`
}

// PipelineOperation defines an operation of a pipeline.
type PipelineOperation struct {
	Name string `mapstructure:"name"`
}

// PipelineExporter defines an exporter of a pipeline.
type PipelineExporter struct {
	Name string `mapstructure:"name"`
}

// NamedEntity defines a configuration entity that has a name.
type NamedEntity interface {
	GetName() string
}

// ReceiverCfg is the configuration of a receiver. Specific receivers
// must implement this interface and will typically embed ReceiverCommon
// struct or a struct that extends it.
type ReceiverCfg interface {
	NamedEntity
}

// TypeKeyName must match Type field key names for Receiver and Exporter.
const TypeKeyName = "type"

// ExporterCfg is the configuration of an exporter. Specific exporters
// must implement this interface and will typically embed ExporterCommon
// struct or a struct that extends it.
type ExporterCfg interface {
	NamedEntity
}

// OptionCfg is the configuration of a processing option. Specific options
// must implement this interface and will typically embed OptionCommon
// struct or a struct that extends it.
type OptionCfg interface {
	NamedEntity
}

// Below are common setting structs for Receivers, Exporters and Options.
// These are helper structs which you can use to implement your specific
// receiver/exporter/option config storage.

// ReceiverCommon defines common settings for a single-protocol receiver configuration.
// Specific receivers can embed this struct and extend it with more fields if needed.
type ReceiverCommon struct {
	Type    string `mapstructure:"type"`
	Enabled bool   `mapstructure:"enabled"`
	Name    string `mapstructure:"name"`
	Address string `mapstructure:"address"`
	Port    int    `mapstructure:"port"`
}

// GetName returns receiver name.
func (cfg *ReceiverCommon) GetName() string {
	return cfg.Name
}

// ExporterCommon defines common settings for an exporter configuration.
// Specific exporters can embed this struct and extend it with more fields if needed.
type ExporterCommon struct {
	Type    string `mapstructure:"type"`
	Enabled bool   `mapstructure:"enabled"`
	Name    string `mapstructure:"name"`
}

// MultiReceiverCommon defines common settings for a multi-protocol receiver configuration.
// Specific receivers can embed this struct and extend it with more fields. Note that
// Protocols field is not defined, you need to define it with a specific type.
type MultiReceiverCommon struct {
	Type string `mapstructure:"type"`
	Name string `mapstructure:"name"`
}

// GetName returns receiver name.
func (cfg *MultiReceiverCommon) GetName() string {
	return cfg.Name
}

// ProtocolReceiver defines a protocol of multi-protocol receiver.
// Specific receivers can embed this struct and extend it with more fields if needed.
type ProtocolReceiver struct {
	Protocol string `mapstructure:"protocol"`
	Enabled  bool   `mapstructure:"enabled"`
	Address  string `mapstructure:"address"`
	Port     int    `mapstructure:"port"`
}

// OptionCommon defines common settings for an option configuration
// Specific options can embed this struct and extend it with more fields if needed.
type OptionCommon struct {
	Type    string `mapstructure:"type"`
	Enabled bool   `mapstructure:"enabled"`
	Name    string `mapstructure:"name"`
}

// GetName returns option name.
func (cfg *OptionCommon) GetName() string {
	return cfg.Name
}

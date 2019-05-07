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

// Package configv2 implements loading of configuration V2 from Viper configuration.
// The implementation relies on registered factories that allow creating
// default configuration for each type of receiver/exporter/option.
package configv2

import (
	"fmt"

	"github.com/census-instrumentation/opencensus-service/internal/models"
	"github.com/spf13/viper"
)

// These are errors that can be returned by Load(). Note that error codes are not part
// of Load()'s public API, they are for internal unit testing only.
type configErrorCode int

const (
	_ configErrorCode = iota // skip 0, start errors codes from 1
	errUnknownReceiverType
	errUnknownExporterType
	errUnknownOptionType
	errMissingReceiverType
	errMissingExporterType
	errMissingOptionType
	errDuplicateReceiverName
	errDuplicateExporterName
	errDuplicateOptionName
	errMissingPipelines
	errMissingPipelineName
	errPipelineMustHaveReceiver
	errPipelineMustHaveExporter
	errPipelineMustHaveOperations
	errPipelineReceiverNotExists
	errPipelineOptionNotExists
	errPipelineExporterNotExists
	errMetricPipelineCannotHaveOperations
	errUnmarshalError
)

type configError struct {
	msg  string          // human readable error message
	code configErrorCode // internal error code
}

// ensure configError implements error interface.
var _ error = (*configError)(nil)

func (e *configError) Error() string {
	return e.msg
}

// Load loads a ConfigV2 from Viper
func Load(v *viper.Viper) (*models.ConfigV2, error) {

	// Unmarshal everything that can be automatically unmarshaled (currently only pipelines)
	var config models.ConfigV2
	if err := v.Unmarshal(&config); err != nil {
		return nil, err
	}

	// Load the rest of entities that are not automatically unmarshaled: receivers, exporters, options.

	receivers, err := loadReceivers(v)
	if err != nil {
		return nil, err
	}
	config.Receivers = receivers

	exporters, err := loadExporters(v)
	if err != nil {
		return nil, err
	}
	config.Exporters = exporters

	options, err := loadOptions(v)
	if err != nil {
		return nil, err
	}
	config.Options = options

	// Config is loaded. Now validate it.

	if err := validateConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

func loadReceivers(v *viper.Viper) ([]models.ReceiverCfg, error) {
	// Get the list of all "receivers" items from config source
	subs, err := getViperSubSequenceOfMaps(v, models.ReceiversKeyName)
	if err != nil {
		return nil, err
	}

	// Iterate over receivers and create a config for each
	receivers := make([]models.ReceiverCfg, 0)
	for _, sub := range subs {
		// Find receiver factory based on "type" that we read from config source
		typeStr := sub.GetString(models.TypeKeyName)
		if typeStr == "" {
			return nil, &configError{code: errMissingReceiverType, msg: "missing receiver type"}
		}

		factory := models.GetReceiverFactory(typeStr)
		if factory == nil {
			return nil, &configError{code: errUnknownReceiverType, msg: fmt.Sprintf("unknown receiver type %q", typeStr)}
		}

		// Create the default config for this receiver
		receiverCfg := factory.CreateDefaultConfig()

		// Now that the default config struct is created we can Unmarshal into it
		// and it will apply user-defined config on top of the default.
		if err := sub.Unmarshal(receiverCfg); err != nil {
			return nil, &configError{
				code: errUnmarshalError,
				msg:  fmt.Sprintf("error reading settings for receiver type %q: %v", typeStr, err),
			}
		}
		receivers = append(receivers, receiverCfg)
	}

	return receivers, nil
}

func loadExporters(v *viper.Viper) ([]models.ExporterCfg, error) {
	// Get the list of all "exporters" items from config source
	subs, err := getViperSubSequenceOfMaps(v, models.ExportersKeyName)
	if err != nil {
		return nil, err
	}

	// Iterate over exporters and create a config for each
	exporters := make([]models.ExporterCfg, 0)
	for _, sub := range subs {
		// Find exporter factory based on "type" that we read from config source
		typeStr := sub.GetString(models.TypeKeyName)
		if typeStr == "" {
			return nil, &configError{code: errMissingExporterType, msg: "missing exporter type"}
		}

		factory := models.GetExporterFactory(typeStr)
		if factory == nil {
			return nil, &configError{code: errUnknownExporterType, msg: fmt.Sprintf("unknown exporter type %q", typeStr)}
		}

		// Create the default config for this exporter
		exporterCfg := factory.CreateDefaultConfig()

		// Now that the default config struct is created we can Unmarshal into it
		// and it will apply user-defined config on top of the default.
		if err := sub.Unmarshal(exporterCfg); err != nil {
			return nil, &configError{
				code: errUnmarshalError,
				msg:  fmt.Sprintf("error reading settings for exporter type %q: %v", typeStr, err),
			}
		}
		exporters = append(exporters, exporterCfg)
	}

	return exporters, nil
}

func loadOptions(v *viper.Viper) ([]models.OptionCfg, error) {
	// Get the list of all "options" items from config source. Note that we may need
	// to rename "options" to "operations" if we converge on that after an ongoing discussion.
	subs, err := getViperSubSequenceOfMaps(v, models.OptionsKeyName)
	if err != nil {
		return nil, err
	}

	// Iterate over options and create a config for each
	options := make([]models.OptionCfg, 0)
	for _, sub := range subs {
		// Find option factory based on "type" that we read from config source
		typeStr := sub.GetString(models.TypeKeyName)
		if typeStr == "" {
			return nil, &configError{code: errMissingOptionType, msg: "missing option type"}
		}

		factory := models.GetOptionFactory(typeStr)
		if factory == nil {
			return nil, &configError{code: errUnknownOptionType, msg: fmt.Sprintf("unknown option type %q", typeStr)}
		}

		// Create the default config for this options
		optionCfg := factory.CreateDefaultConfig()

		// Now that the default config struct is created we can Unmarshal into it
		// and it will apply user-defined config on top of the default.
		if err := sub.Unmarshal(optionCfg); err != nil {
			return nil, &configError{
				code: errUnmarshalError,
				msg:  fmt.Sprintf("error reading settings for option type %q: %v", typeStr, err),
			}
		}
		options = append(options, optionCfg)
	}

	return options, nil
}

func validateConfig(cfg *models.ConfigV2) error {
	// This function performs basic validation of configuration.
	// There may be more subtle invalid cases that we currently don't check for but which
	// we may want to add in the future (e.g. disallowing receiving and exporting on the
	// same endpoint).

	if err := validateReceivers(cfg.Receivers); err != nil {
		return err
	}

	if err := validateExporters(cfg.Exporters); err != nil {
		return err
	}

	if err := validateOptions(cfg.Options); err != nil {
		return err
	}

	if err := validatePipelines(cfg); err != nil {
		return err
	}

	return nil
}

func validateReceivers(receivers []models.ReceiverCfg) error {
	m := make(map[string]bool)
	for _, r := range receivers {
		if m[r.GetName()] {
			return &configError{code: errDuplicateReceiverName, msg: fmt.Sprintf("duplicate receiver name %q", r.GetName())}
		}
		m[r.GetName()] = true
	}

	return nil
}

func validateExporters(exporters []models.ExporterCfg) error {
	m := make(map[string]bool)
	for _, r := range exporters {
		if m[r.GetName()] {
			return &configError{code: errDuplicateExporterName, msg: fmt.Sprintf("duplicate exporter name %q", r.GetName())}
		}
		m[r.GetName()] = true
	}

	return nil
}

func validateOptions(options []models.OptionCfg) error {
	m := make(map[string]bool)
	for _, r := range options {
		if m[r.GetName()] {
			return &configError{code: errDuplicateOptionName, msg: fmt.Sprintf("duplicate option name %q", r.GetName())}
		}
		m[r.GetName()] = true
	}

	return nil
}

func validatePipelines(cfg *models.ConfigV2) error {
	// Must have at least one pipeline
	if len(cfg.Pipelines.Traces) < 1 && len(cfg.Pipelines.Metrics) < 1 {
		return &configError{code: errMissingPipelines, msg: "must have at least one pipeline"}
	}

	// Validate trace pipelines
	for _, pipeline := range cfg.Pipelines.Traces {
		// Perform common pipeline validation
		if err := validatePipeline(cfg, pipeline); err != nil {
			return err
		}
		// Also validate options
		if err := validatePipelineOptions(cfg, pipeline); err != nil {
			return err
		}
	}

	for _, pipeline := range cfg.Pipelines.Metrics {
		// For metrics pipeline we only do common validation since it cannot have options
		if err := validatePipeline(cfg, pipeline); err != nil {
			return err
		}

		if len(pipeline.Operations) > 0 {
			return &configError{
				code: errMetricPipelineCannotHaveOperations,
				msg:  fmt.Sprintf("metrics pipeline %q cannot have ordered operation", pipeline.Name),
			}
		}
	}
	return nil
}

func validatePipeline(cfg *models.ConfigV2, pipeline *models.Pipeline) error {
	if pipeline.Name == "" {
		return &configError{code: errMissingPipelineName, msg: "pipeline must have a name"}
	}

	if err := validatePipelineReceivers(cfg, pipeline); err != nil {
		return err
	}

	if err := validatePipelineExporters(cfg, pipeline); err != nil {
		return err
	}

	return nil
}

func validatePipelineReceivers(cfg *models.ConfigV2, pipeline *models.Pipeline) error {
	if len(pipeline.Receivers) == 0 {
		return &configError{
			code: errPipelineMustHaveReceiver,
			msg:  fmt.Sprintf("pipeline %q must have at least one receiver", pipeline.Name),
		}
	}

	// Validate pipeline receiver name references
	for _, ref := range pipeline.Receivers {
		// Check that the name referenced in the pipeline's Receivers exists in the top-level Receivers
		found := false
		for _, rcv := range cfg.Receivers {
			if rcv.GetName() == ref.Name {
				found = true
				break
			}
		}
		if !found {
			return &configError{
				code: errPipelineReceiverNotExists,
				msg:  fmt.Sprintf("pipeline %q references receiver %q which does not exists", pipeline.Name, ref.Name),
			}
		}
	}

	return nil
}

func validatePipelineExporters(cfg *models.ConfigV2, pipeline *models.Pipeline) error {
	if len(pipeline.Exporters) == 0 {
		return &configError{
			code: errPipelineMustHaveExporter,
			msg:  fmt.Sprintf("pipeline %q must have at least one exporter", pipeline.Name),
		}
	}

	// Validate pipeline exporter name references
	for _, ref := range pipeline.Exporters {
		// Check that the name referenced in the pipeline's Exporters exists in the top-level Exporters
		found := false
		for _, exp := range cfg.Exporters {
			if exp.GetName() == ref.Name {
				found = true
				break
			}
		}
		if !found {
			return &configError{
				code: errPipelineExporterNotExists,
				msg:  fmt.Sprintf("pipeline %q references exporter %q which does not exists", pipeline.Name, ref.Name),
			}
		}
	}

	return nil
}

func validatePipelineOptions(cfg *models.ConfigV2, pipeline *models.Pipeline) error {
	if len(pipeline.Operations) == 0 {
		return &configError{
			code: errPipelineMustHaveOperations,
			msg:  fmt.Sprintf("pipeline %q must have at least one ordered operation", pipeline.Name),
		}
	}

	// Validate pipeline option name references
	for _, ref := range pipeline.Operations {
		// Check that the name referenced in the pipeline's Operations exists in the top-level Options
		found := false
		for _, exp := range cfg.Options {
			if exp.GetName() == ref.Name {
				found = true
				break
			}
		}
		if !found {
			return &configError{
				code: errPipelineOptionNotExists,
				msg:  fmt.Sprintf("pipeline %q references option %s which does not exists", pipeline.Name, ref.Name),
			}
		}
	}

	return nil
}

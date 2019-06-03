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

package opencensusexporter

import (
	"github.com/census-instrumentation/opencensus-service/internal/configmodels"
	"github.com/census-instrumentation/opencensus-service/internal/factories"
)

var _ = factories.RegisterExporterFactory(&ExporterFactory{})

const (
	// The value of "type" key in configuration.
	typeStr = "opencensus"
)

// ExporterFactory is the factory for OpenCensus exporter.
type ExporterFactory struct {
}

// Type gets the type of the Exporter config created by this factory.
func (f *ExporterFactory) Type() string {
	return typeStr
}

// CreateDefaultConfig creates the default configuration for exporter.
func (f *ExporterFactory) CreateDefaultConfig() configmodels.Exporter {
	return &ConfigV2{
		ExporterSettings: configmodels.ExporterSettings{},
		Headers:          map[string]string{},
	}
}

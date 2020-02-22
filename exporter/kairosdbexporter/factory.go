// Copyright 2020, OpenCensus Authors
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

package kairosdbexporter

import (
	"fmt"

	"github.com/census-instrumentation/opencensus-service/consumer"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

const (
	// The value of "type" key in configuration.
	typeStr = "kairosdb"
)

// KairosDbExportersFromViper instantiates the metrics consumers based on KairosDB API.
func KairosDbExportersFromViper(v *viper.Viper) (tps []consumer.TraceConsumer, mps []consumer.MetricsConsumer, doneFns []func() error, err error) {
	var kairosCfg wrapperCfg
	if err := v.Unmarshal(&kairosCfg); err != nil {
		panic(err)
	}

	if kairosCfg.Config == nil {
		errMsg := fmt.Sprintf("invalid configuration for the kairosdb exporter %v", v)
		panic(errMsg)
	}

	if kairosCfg.Config.HTTPConfig == nil {
		errMsg := fmt.Sprintf("no valid http config available for the kairosdb exporter %v", v)
		panic(errMsg)
	}

	loggerCfg := zap.NewProductionConfig()
	loggerCfg.Level.SetLevel(kairosCfg.Config.GetZapLevel())
	logger, err := loggerCfg.Build()
	if err != nil {
		panic(err)
	}

	exporter := newExporter(logger, kairosCfg.Config)
	mps = append(mps, exporter)
	return
}

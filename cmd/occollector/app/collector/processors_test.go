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

package collector

import (
	"reflect"
	"testing"

	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/census-instrumentation/opencensus-service/processor/addattributesprocessor"
	"github.com/census-instrumentation/opencensus-service/processor/attributekeyprocessor"
	"github.com/census-instrumentation/opencensus-service/processor/multiconsumer"
	"github.com/census-instrumentation/opencensus-service/processor/processortest"
	"github.com/census-instrumentation/opencensus-service/processor/tracesamplerprocessor"
)

func Test_startProcessor(t *testing.T) {
	tests := []struct {
		name          string
		setupViperCfg func() *viper.Viper
		wantExamplar  func(t *testing.T) interface{}
	}{
		{
			name: "incomplete_global_attrib_config",
			setupViperCfg: func() *viper.Viper {
				v := viper.New()
				v.Set("logging-exporter", true)
				v.Set("global.attributes.overwrite", true)
				return v
			},
			wantExamplar: func(t *testing.T) interface{} {
				return multiconsumer.NewTraceProcessor(nil)
			},
		},
		{
			name: "global_attrib_config_values",
			setupViperCfg: func() *viper.Viper {
				v := viper.New()
				v.Set("logging-exporter", true)
				v.Set("global.attributes.values", map[string]interface{}{"foo": "bar"})
				return v
			},
			wantExamplar: func(t *testing.T) interface{} {
				nopProcessor := processortest.NewNopTraceProcessor(nil)
				addAttributesProcessor, err := addattributesprocessor.NewTraceProcessor(nopProcessor)
				if err != nil {
					t.Fatalf("addattributesprocessor.NewTraceProcessor() = %v", err)
				}
				return addAttributesProcessor
			},
		},
		{
			name: "global_attrib_config_key_mapping",
			setupViperCfg: func() *viper.Viper {
				v := viper.New()
				v.Set("logging-exporter", true)
				v.Set("global.attributes.key-mapping",
					[]map[string]interface{}{
						{
							"key":         "foo",
							"replacement": "bar",
						},
					})
				return v
			},
			wantExamplar: func(t *testing.T) interface{} {
				nopProcessor := processortest.NewNopTraceProcessor(nil)
				attributeKeyProcessor, err := attributekeyprocessor.NewTraceProcessor(nopProcessor)
				if err != nil {
					t.Fatalf("attributekeyprocessor.NewTraceProcessor() = %v", err)
				}
				return attributeKeyProcessor
			},
		},
		{
			name: "sampling_config_trace_sampler",
			setupViperCfg: func() *viper.Viper {
				v := viper.New()
				v.Set("logging-exporter", true)
				v.Set("sampling.mode", "head")
				v.Set("sampling.policies.probabilistic.configuration.sampling-percentage", 5)
				return v
			},
			wantExamplar: func(t *testing.T) interface{} {
				nopProcessor := processortest.NewNopTraceProcessor(nil)
				tracesamplerprocessor, err := tracesamplerprocessor.NewTraceProcessor(nopProcessor, tracesamplerprocessor.TraceSamplerCfg{})
				if err != nil {
					t.Fatalf("tracesamplerprocessor.NewTraceProcessor() = %v", err)
				}
				return tracesamplerprocessor
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			consumer, closeFns := startProcessor(tt.setupViperCfg(), zap.NewNop())
			if consumer == nil {
				t.Errorf("startProcessor() got nil consumer")
			}
			consumerExamplar := tt.wantExamplar(t)
			if reflect.TypeOf(consumer) != reflect.TypeOf(consumerExamplar) {
				t.Errorf("startProcessor() got consumer type %q want %q",
					reflect.TypeOf(consumer),
					reflect.TypeOf(consumerExamplar))
			}
			for _, closeFn := range closeFns {
				closeFn()
			}
		})
	}
}

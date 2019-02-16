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

package exporterparser

// TODO: (@odeke-em) file an issue at the official Honeycomb repository to
// ask them to make an exporter that uses OpenCensus-Proto instead of OpenCensus-Go.

import (
	"context"

	"github.com/honeycombio/opencensus-exporter/honeycomb"
	"github.com/spf13/viper"

	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/exporter"
)

type honeycombConfig struct {
	WriteKey    string `mapstructure:"write_key"`
	DatasetName string `mapstructure:"dataset_name"`
}

// HoneycombTraceExportersFromViper unmarshals the viper and returns an exporter.TraceExporter
// targeting Honeycomb according to the configuration settings.
func HoneycombTraceExportersFromViper(v *viper.Viper) (tes []exporter.TraceExporter, mes []exporter.MetricsExporter, doneFns []func() error, err error) {
	var cfg struct {
		Honeycomb *honeycombConfig `mapstructure:"honeycomb"`
	}
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, nil, nil, err
	}

	hc := cfg.Honeycomb
	if hc == nil {
		return nil, nil, nil, nil
	}

	rawExp := honeycomb.NewExporter(hc.WriteKey, hc.DatasetName)
	hce := &honeycombExporter{exporter: rawExp}

	tes = append(tes, hce)
	doneFns = append(doneFns, func() error {
		rawExp.Close()
		return nil
	})
	return
}

type honeycombExporter struct {
	exporter *honeycomb.Exporter
}

func (hce *honeycombExporter) ExportSpans(ctx context.Context, td data.TraceData) error {
	return ocProtoSpansToOCSpanDataInstrumented(ctx, "honeycomb", hce.exporter, td)
}

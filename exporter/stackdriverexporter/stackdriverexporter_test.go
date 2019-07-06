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

package stackdriverexporter

import (
	"context"
	"reflect"
	"testing"
	"time"

	"contrib.go.opencensus.io/exporter/stackdriver"
	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
	resourcepb "github.com/census-instrumentation/opencensus-proto/gen-go/resource/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/exporter/exportertest"
	"github.com/census-instrumentation/opencensus-service/internal/config/viperutils"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/spf13/viper"
	"go.opencensus.io/trace"
)

type fakeStackdriverExporter struct {
	spanData []*trace.SpanData
}

var _ stackdriverExporterInterface = (*fakeStackdriverExporter)(nil)

func (*fakeStackdriverExporter) ExportMetricsProto(ctx context.Context, node *commonpb.Node, rsc *resourcepb.Resource, metrics []*metricspb.Metric) error {
	return nil
}

func (exp *fakeStackdriverExporter) ExportSpan(sd *trace.SpanData) {
	exp.spanData = append(exp.spanData, sd)
}

func (*fakeStackdriverExporter) Flush() {
}

func fakeStackdriverNewExporter(opts stackdriver.Options) (*stackdriver.Exporter, error) {
	return nil, nil
}

func TestStackriverExporter(t *testing.T) {
	v := viper.New()
	configYAML := []byte(`
stackdriver:
  project: 'test-project'
  enable_tracing: true
  enable_metrics: true
  metric_prefix: 'test-metric-prefix'`)
	err := viperutils.LoadYAMLBytes(v, configYAML)
	exp := fakeStackdriverExporter{}
	tps, mps, doneFns, err := stackdriverTraceExportersFromViperInternal(v, func(opts stackdriver.Options) (stackdriverExporterInterface, error) {
		if opts.ProjectID != "test-project" {
			t.Errorf("Unexpected ProjectID: %v", opts.ProjectID)
		}
		if opts.MetricPrefix != "test-metric-prefix" {
			t.Errorf("Unexpected MetricPrefix: %v", opts.MetricPrefix)
		}
		return &exp, nil
	})
	if len(tps) != 1 {
		t.Errorf("Unexpected TraceConsumer count: %v", len(tps))
	}
	if len(mps) != 1 {
		t.Errorf("Unexpected MetricsConsumer count: %v", len(mps))
	}
	if len(doneFns) != 1 {
		t.Errorf("Unexpected doneFns count: %v", len(doneFns))
	}
	if err != nil {
		t.Errorf("Expected nil erorr, got %v", err)
	}

	td := data.TraceData{
		Node: &commonpb.Node{
			LibraryInfo: &commonpb.LibraryInfo{
				Language:           commonpb.LibraryInfo_PYTHON,
				ExporterVersion:    "0.4.1",
				CoreLibraryVersion: "0.3.2",
			},
		},
		Spans: []*tracepb.Span{
			{
				TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x80},
				SpanId:    []byte{0xAF, 0xAE, 0xAD, 0xAC, 0xAB, 0xAA, 0xA9, 0xA8},
				Name:      &tracepb.TruncatableString{Value: "DBSearch"},
				StartTime: &timestamp.Timestamp{Seconds: 1550000001, Nanos: 1},
				EndTime:   &timestamp.Timestamp{Seconds: 1550000002, Nanos: 1},
				Attributes: &tracepb.Span_Attributes{
					AttributeMap: map[string]*tracepb.AttributeValue{
						"cache_hit": {Value: &tracepb.AttributeValue_BoolValue{BoolValue: true}},
					},
				},
			},
		},
		SourceFormat: "oc_trace",
	}
	tps[0].ConsumeTraceData(context.Background(), td)

	want := []*trace.SpanData{
		{
			Name: "DBSearch",
			SpanContext: trace.SpanContext{
				TraceID: trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 128},
				SpanID:  trace.SpanID{175, 174, 173, 172, 171, 170, 169, 168},
			},
			StartTime: time.Unix(1550000001, 1),
			EndTime:   time.Unix(1550000002, 1),
			// Check that the agent label is correctly formed based on the library
			// info given in the Node structure above.
			Attributes: map[string]interface{}{
				"cache_hit": true,
				agentLabel:  "opencensus-python 0.3.2; ocagent-exporter 0.4.1",
			},
		},
	}
	got := exp.spanData
	if !reflect.DeepEqual(got, want) {
		gj, wj := exportertest.ToJSON(got), exportertest.ToJSON(want)
		t.Errorf("Incorrect exported SpanData\nGot:\n\t%s\nWant:\n\t%s", gj, wj)
	}
}

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

package stackdriverexporter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"contrib.go.opencensus.io/exporter/stackdriver"
	"github.com/spf13/viper"
	"go.opencensus.io/trace"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
	resourcepb "github.com/census-instrumentation/opencensus-proto/gen-go/resource/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"github.com/census-instrumentation/opencensus-service/consumer"
	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/exporter/exporterhelper"
	"github.com/census-instrumentation/opencensus-service/exporter/exporterwrapper"
)

const agentLabel = "g.co/agent"

type stackdriverConfig struct {
	ProjectID     string `mapstructure:"project,omitempty"`
	EnableTracing bool   `mapstructure:"enable_tracing,omitempty"`
	EnableMetrics bool   `mapstructure:"enable_metrics,omitempty"`
	MetricPrefix  string `mapstructure:"metric_prefix,omitempty"`
}

// This interface and factory function type enable passing a fake Stackdriver
// exporter for a unit test.
type stackdriverExporterInterface interface {
	exporterwrapper.OCSpanExporter
	ExportMetricsProto(ctx context.Context, node *commonpb.Node, rsc *resourcepb.Resource, metrics []*metricspb.Metric) error
	Flush()
}
type stackdriverExporterFactory = func(o stackdriver.Options) (stackdriverExporterInterface, error)

var _ stackdriverExporterInterface = (*stackdriver.Exporter)(nil)

// TODO: Add metrics support to the exporterwrapper.
type stackdriverExporter struct {
	exporter stackdriverExporterInterface
}

var _ consumer.MetricsConsumer = (*stackdriverExporter)(nil)

// StackdriverTraceExportersFromViper unmarshals the viper and returns an consumer.TraceConsumer targeting
// Stackdriver according to the configuration settings.
func StackdriverTraceExportersFromViper(v *viper.Viper) (tps []consumer.TraceConsumer, mps []consumer.MetricsConsumer, doneFns []func() error, err error) {
	return stackdriverTraceExportersFromViperInternal(v, func(o stackdriver.Options) (stackdriverExporterInterface, error) {
		return stackdriver.NewExporter(o)
	})
}

func stackdriverTraceExportersFromViperInternal(v *viper.Viper, sef stackdriverExporterFactory) (tps []consumer.TraceConsumer, mps []consumer.MetricsConsumer, doneFns []func() error, err error) {
	var cfg struct {
		Stackdriver *stackdriverConfig `mapstructure:"stackdriver"`
	}
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, nil, nil, err
	}
	sc := cfg.Stackdriver
	if sc == nil {
		return nil, nil, nil, nil
	}
	if !sc.EnableTracing && !sc.EnableMetrics {
		return nil, nil, nil, nil
	}

	// TODO:  For each ProjectID, create a different exporter
	// or at least a unique Stackdriver client per ProjectID.

	sde, serr := sef(stackdriver.Options{
		// If the project ID is an empty string, it will be set by default based on
		// the project this is running on in GCP.
		ProjectID: sc.ProjectID,

		MetricPrefix: sc.MetricPrefix,

		// Stackdriver Metrics mandates a minimum of 60 seconds for
		// reporting metrics. We have to enforce this as per the advisory
		// at https://cloud.google.com/monitoring/custom-metrics/creating-metrics#writing-ts
		// which says:
		//
		// "If you want to write more than one point to the same time series, then use a separate call
		//  to the timeSeries.create method for each point. Don't make the calls faster than one time per
		//  minute. If you are adding data points to different time series, then there is no rate limitation."
		BundleDelayThreshold: 61 * time.Second,
	})
	if serr != nil {
		return nil, nil, nil, fmt.Errorf("Cannot configure Stackdriver Trace exporter: %v", serr)
	}

	exp := &stackdriverExporter{
		exporter: sde,
	}

	sdte, err := exporterhelper.NewTraceExporter(
		"stackdriver_trace",
		exp.pushTraceData,
		exporterhelper.WithSpanName("ocservice.exporter.Stackdriver.ConsumeTraceData"),
		exporterhelper.WithRecordMetrics(true),
	)

	if err != nil {
		return nil, nil, nil, err
	}

	// TODO: Examine "contrib.go.opencensus.io/exporter/stackdriver" to see
	// if trace.ExportSpan was constraining and if perhaps the Stackdriver
	// upload can use the context and information from the Node.
	if sc.EnableTracing {
		tps = append(tps, sdte)
	}

	if sc.EnableMetrics {
		mps = append(mps, exp)
	}

	doneFns = append(doneFns, func() error {
		sde.Flush()
		return nil
	})
	return
}

// ExportSpans is the method that translates OpenCensus-Proto Traces into AWS X-Ray spans.
// It uniquely maintains
func (sde *stackdriverExporter) pushTraceData(ctx context.Context, td data.TraceData) (int, error) {
	setAgentLabelFromNode(td)
	return exporterwrapper.PushOcProtoSpansToOCTraceExporter(sde.exporter, td)
}

func setAgentLabelFromNode(td data.TraceData) {
	if td.Node == nil {
		return
	}
	li := td.Node.GetLibraryInfo()
	if li == nil {
		return
	}
	agent := agentForLibraryInfo(li)
	agentVal := &tracepb.AttributeValue{
		Value: &tracepb.AttributeValue_StringValue{
			StringValue: &tracepb.TruncatableString{Value: agent},
		},
	}
	for _, span := range td.Spans {
		if span.Attributes == nil {
			span.Attributes = &tracepb.Span_Attributes{}
		}
		if span.Attributes.AttributeMap == nil {
			span.Attributes.AttributeMap = map[string]*tracepb.AttributeValue{}
		}
		span.GetAttributes().GetAttributeMap()[agentLabel] = agentVal
	}
}

func (sde *stackdriverExporter) ConsumeMetricsData(ctx context.Context, md data.MetricsData) error {
	ctx, span := trace.StartSpan(ctx,
		"opencensus.service.exporter.stackdriver.ExportMetricsData",
		trace.WithSampler(trace.NeverSample()))
	defer span.End()

	var setErrorOnce sync.Once

	err := sde.exporter.ExportMetricsProto(ctx, md.Node, md.Resource, md.Metrics)
	if err != nil {
		setErrorOnce.Do(func() {
			span.SetStatus(trace.Status{Code: trace.StatusCodeInternal, Message: err.Error()})
		})

		span.Annotate([]trace.Attribute{
			trace.StringAttribute("error", err.Error()),
		}, "Error encountered")
	}

	return nil
}

func agentForLibraryInfo(li *commonpb.LibraryInfo) string {
	langName := strings.ToLower(li.Language.String())
	// The exporter must be the OpenCensus agent exporter because the spans
	// have been written to the OpenCensus service.
	return "opencensus-" + langName + " " + li.CoreLibraryVersion + "; ocagent-exporter " + li.ExporterVersion
}

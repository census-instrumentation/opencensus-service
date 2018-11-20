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

package exporter

import (
	"context"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
	resourcepb "github.com/census-instrumentation/opencensus-proto/gen-go/resource/v1"
	"github.com/census-instrumentation/opencensus-service/receiver"
)

// MetricsExporter is a interface that receives OpenCensus metrics, converts it as needed, and
// sends it to different destinations.
//
// ExportMetrics receives OpenCensus proto metrics for processing by the exporter.
type MetricsExporter interface {
	ExportMetrics(ctx context.Context, node *commonpb.Node, resource *resourcepb.Resource, metrics ...*metricspb.Metric) error
}

// MetricsExporterSink is an interface connecting a MetricsReceiverSink and
// an exporter.MetricsExporter. The sink gets data in different serialization formats,
// transforms it to OpenCensus in memory data and sends it to the exporter.
type MetricsExporterSink interface {
	MetricsExporter
	receiver.MetricsReceiverSink
}

type metricsExporters []MetricsExporter

// ExportMetrics exports the metrics to all exporters wrapped by the current one.
func (mes metricsExporters) ExportMetrics(ctx context.Context, node *commonpb.Node, resource *resourcepb.Resource, metrics ...*metricspb.Metric) error {
	for _, te := range mes {
		_ = te.ExportMetrics(ctx, node, resource, metrics...)
	}
	return nil
}

// ReceiveMetrics receives the metric data in the protobuf format, translates it, and forwards the transformed
// metric data to all metrics exporters wrapped by the current one.
func (mes metricsExporters) ReceiveMetrics(ctx context.Context, node *commonpb.Node, resource *resourcepb.Resource, metrics ...*metricspb.Metric) (*receiver.MetricsReceiverAcknowledgement, error) {
	for _, te := range mes {
		_ = te.ExportMetrics(ctx, node, resource, metrics...)
	}

	ack := &receiver.MetricsReceiverAcknowledgement{
		SavedMetrics: uint64(len(metrics)),
	}
	return ack, nil
}

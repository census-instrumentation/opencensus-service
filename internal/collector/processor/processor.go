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

package processor

import (
	"context"

	"github.com/census-instrumentation/opencensus-service/observability"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/internal/collector/telemetry"
)

// SpanProcessor handles batches of spans converted to OpenCensus proto format.
type SpanProcessor interface {
	// ProcessSpans processes spans and return with the number of spans that failed and an error.
	ProcessSpans(ctx context.Context, td data.TraceData) error
	// TODO: (@pjanotti) For shutdown improvement, the interface needs a method to attempt that.
}

// Keys and stats for telemetry.
var (
	TagServiceNameKey, _  = tag.NewKey("service")
	TagExporterNameKey, _ = tag.NewKey("exporter")

	StatReceivedSpanCount = stats.Int64("spans_received", "counts the number of spans received", stats.UnitDimensionless)
	StatDroppedSpanCount  = stats.Int64("spans_dropped", "counts the number of spans dropped", stats.UnitDimensionless)
)

// MetricTagKeys returns the metric tag keys according to the given telemetry level.
func MetricTagKeys(level telemetry.Level) []tag.Key {
	var tagKeys []tag.Key
	switch level {
	case telemetry.Detailed:
		tagKeys = append(tagKeys, TagServiceNameKey)
		fallthrough
	case telemetry.Normal:
		tagKeys = append(tagKeys, observability.TagKeyReceiver)
		fallthrough
	case telemetry.Basic:
		tagKeys = append(tagKeys, TagExporterNameKey)
		break
	default:
		return nil
	}

	return tagKeys
}

// MetricViews return the metrics views according to given telemetry level.
func MetricViews(level telemetry.Level) []*view.View {
	tagKeys := MetricTagKeys(level)
	if tagKeys == nil {
		return nil
	}

	// There are some metrics enabled, return the views.
	receivedBatchesView := &view.View{
		Name:        "batches_received",
		Measure:     StatReceivedSpanCount,
		Description: "The number of span batches received.",
		TagKeys:     tagKeys,
		Aggregation: view.Count(),
	}
	droppedBatchesView := &view.View{
		Name:        "batches_dropped",
		Measure:     StatDroppedSpanCount,
		Description: "The number of span batches dropped.",
		TagKeys:     tagKeys,
		Aggregation: view.Count(),
	}
	receivedSpansView := &view.View{
		Name:        StatReceivedSpanCount.Name(),
		Measure:     StatReceivedSpanCount,
		Description: "The number of spans received.",
		TagKeys:     tagKeys,
		Aggregation: view.Sum(),
	}
	droppedSpansView := &view.View{
		Name:        StatDroppedSpanCount.Name(),
		Measure:     StatDroppedSpanCount,
		Description: "The number of spans dropped.",
		TagKeys:     tagKeys,
		Aggregation: view.Sum(),
	}

	return []*view.View{receivedBatchesView, droppedBatchesView, receivedSpansView, droppedSpansView}
}

// ServiceNameForNode gets the service name for a specified node. Used for metrics.
func ServiceNameForNode(node *commonpb.Node) string {
	var serviceName string
	if node == nil {
		serviceName = "<nil-batch-node>"
	} else if node.ServiceInfo == nil {
		serviceName = "<nil-service-info>"
	} else if node.ServiceInfo.Name == "" {
		serviceName = "<empty-service-info-name>"
	} else {
		serviceName = node.ServiceInfo.Name
	}
	return serviceName
}

// StatsTagsForBatch gets the stat tags based on the specified processorName and serviceName.
func StatsTagsForBatch(processorName, serviceName string) []tag.Mutator {
	statsTags := []tag.Mutator{
		tag.Upsert(TagServiceNameKey, serviceName),
		tag.Upsert(TagExporterNameKey, processorName),
	}

	return statsTags
}

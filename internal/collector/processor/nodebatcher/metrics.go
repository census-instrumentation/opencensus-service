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

package nodebatcher

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"github.com/census-instrumentation/opencensus-service/internal/collector/processor"
	"github.com/census-instrumentation/opencensus-service/internal/collector/telemetry"
)

var (
	statBatchSize         = stats.Int64("batch_size", "Size of batches sent from the batcher (in span)", stats.UnitDimensionless)
	statAddNodeBatches    = stats.Int64("add_node_batches", "Count of nodes that have been added to batching.", stats.UnitDimensionless)
	statRemoveNodeBatches = stats.Int64("remove_node_batches", "Number of nodes that have been removed from batching.", stats.UnitDimensionless)

	statSendByBatchSize     = stats.Int64("batch_size_trigger_send", "Number of times the batch was sent due to a size trigger", stats.UnitDimensionless)
	statSendByTimeout       = stats.Int64("timeout_trigger_send", "Number of times the batch was sent due to a timeout trigger", stats.UnitDimensionless)
	statAddOnDeadNodeBucket = stats.Int64("removed_node_send", "Number of times the batch was sent due to spans being added being for a no longer active node", stats.UnitDimensionless)
)

// MetricsViews returns the metrics views related to batching
func MetricViews(level telemetry.Level) []*view.View {
	if level == telemetry.None {
		return nil
	}

	tagKeys := processor.MetricTagKeys(level)
	if tagKeys == nil {
		return nil
	}

	exporterTagKeys := []tag.Key{processor.TagExporterNameKey}

	batchSizeAggregation := view.Distribution(10, 25, 50, 75, 100, 250, 500, 750, 1000, 2000, 3000, 4000, 5000, 10000, 20000, 30000, 50000, 100000)

	batchSizeView := &view.View{
		Name:        statBatchSize.Name(),
		Measure:     statBatchSize,
		Description: "The size of batches being sent from the batched exporter",
		TagKeys:     exporterTagKeys,
		Aggregation: batchSizeAggregation,
	}

	addNodeBatchesView := &view.View{
		Name:        statAddNodeBatches.Name(),
		Measure:     statAddNodeBatches,
		Description: "The count of new nodes added to be batched",
		TagKeys:     exporterTagKeys,
		Aggregation: view.Count(),
	}

	removeNodeBatchesView := &view.View{
		Name:        statRemoveNodeBatches.Name(),
		Measure:     statRemoveNodeBatches,
		Description: "The number of nodes removed from batching",
		TagKeys:     exporterTagKeys,
		Aggregation: view.Count(),
	}

	countSendByBatchSizeView := &view.View{
		Name:        statSendByBatchSize.Name(),
		Measure:     statSendByBatchSize,
		Description: "The number of times a batch was sent due to the size cap trigger",
		TagKeys:     tagKeys,
		Aggregation: view.Count(),
	}

	countSendByTimeoutView := &view.View{
		Name:        statSendByTimeout.Name(),
		Measure:     statSendByTimeout,
		Description: "The number of times a batch was sent due to timeout trigger",
		TagKeys:     tagKeys,
		Aggregation: view.Count(),
	}

	countSendByDeadNode := &view.View{
		Name:        statAddOnDeadNodeBucket.Name(),
		Measure:     statAddOnDeadNodeBucket,
		Description: "The number of times a batch was sent due to spans being added for a no longer active node.",
		TagKeys:     tagKeys,
		Aggregation: view.Count(),
	}

	return []*view.View{
		batchSizeView,
		addNodeBatchesView,
		removeNodeBatchesView,
		countSendByBatchSizeView,
		countSendByTimeoutView,
		countSendByDeadNode,
	}
}

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

package prometheusreceiver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agentmetricspb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/metrics/v1"
	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
	resourcepb "github.com/census-instrumentation/opencensus-proto/gen-go/resource/v1"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/timestamp"
)

type bundling struct {
	md          *metricspb.MetricDescriptor
	node        *commonpb.Node
	resource    *resourcepb.Resource
	point       *metricspb.Point
	ts          *timestamp.Timestamp
	labelValues []*metricspb.LabelValue
}

type signatureNodeResourceTriple struct {
	node      *commonpb.Node
	resource  *resourcepb.Resource
	signature string
}

func compactAndCombineMetrics(ms metricsSink, bundlings []*bundling) {
	// Sort all of them by timestamp first.
	sort.Slice(bundlings, func(i, j int) bool {
		bdli, bdlj := bundlings[i], bundlings[j]
		ti := bdli.point.GetTimestamp()
		tj := bdlj.point.GetTimestamp()
		return ti.GetSeconds() < tj.GetSeconds() &&
			ti.GetNanos() < tj.GetNanos()
	})

	// Our goal is to bundle metrics indexed by metricDescriptor
	byMdNodeResourceSignature := make(map[string][]*bundling)
	uniqueOrderedMNRSignatures := make([]string, 0, len(bundlings))
	for _, bdl := range bundlings {
		mdSignature, _ := proto.Marshal(bdl.md)
		nodeResourceSignature := nodeResourceSignature(bdl.node, bdl.resource)
		signature := fmt.Sprintf("%s%s", mdSignature, nodeResourceSignature)
		if _, ok := byMdNodeResourceSignature[signature]; !ok {
			uniqueOrderedMNRSignatures = append(uniqueOrderedMNRSignatures, signature)
		}
		byMdNodeResourceSignature[signature] = append(byMdNodeResourceSignature[signature], bdl)
	}

	byNodeResourceSignature := make(map[string][]*metricspb.Metric)
	uniqueOrderedNRSignatures := make([]*signatureNodeResourceTriple, 0, len(bundlings))
	for _, bdls := range byMdNodeResourceSignature {
		node, resource, metric := createMetricsWithSameMetricDescriptorNodeResource(bdls)
		signature := nodeResourceSignature(node, resource)
		if _, ok := byNodeResourceSignature[signature]; !ok {
			uniqueOrderedNRSignatures = append(uniqueOrderedNRSignatures, &signatureNodeResourceTriple{
				node:      node,
				resource:  resource,
				signature: signature,
			})
		}
		byNodeResourceSignature[signature] = append(byNodeResourceSignature[signature], metric)
	}

	// And finally since we now have the Node+Metric pairs ordered
	for _, snrt := range uniqueOrderedNRSignatures {
		metrics := byNodeResourceSignature[snrt.signature]
		ereq := &agentmetricspb.ExportMetricsServiceRequest{
			Node:     snrt.node,
			Resource: snrt.resource,
			Metrics:  metrics,
		}
		_ = ms.ReceiveMetrics(context.Background(), ereq)
	}
}

func nodeResourceSignature(node *commonpb.Node, resource *resourcepb.Resource) string {
	nodeSignature, _ := proto.Marshal(node)
	resourceSignature, _ := proto.Marshal(resource)
	return fmt.Sprintf("%s%s", nodeSignature, resourceSignature)
}

func labelValuesSignature(labelValues []*metricspb.LabelValue) string {
	buf := new(strings.Builder)
	for _, labelValue := range labelValues {
		buf.WriteString(labelValue.Value)
		fmt.Fprintf(buf, "%t", labelValue.HasValue)
	}
	return buf.String()
}

func createMetricsWithSameMetricDescriptorNodeResource(bundlings []*bundling) (*commonpb.Node, *resourcepb.Resource, *metricspb.Metric) {
	// We've got to index by labelValues now too
	// so that TimeSeries are easily compacted.
	byLabelValuesIndex := make(map[string][]*bundling)
	uniqueOrderedLabelValues := make([]string, 0, len(bundlings))
	for _, bdl := range bundlings {
		signature := labelValuesSignature(bdl.labelValues)
		bdvl, ok := byLabelValuesIndex[signature]
		if !ok {
			uniqueOrderedLabelValues = append(uniqueOrderedLabelValues, signature)
		}
		bdvl = append(bdvl, bdl)
		byLabelValuesIndex[signature] = bdvl
	}

	var timeseries []*metricspb.TimeSeries
	for _, signature := range uniqueOrderedLabelValues {
		bdls := byLabelValuesIndex[signature]
		points := make([]*metricspb.Point, len(bdls))
		for i, bdl := range bdls {
			points[i] = bdl.point
		}

		bhead := bdls[0]
		timeseries = append(timeseries, &metricspb.TimeSeries{
			LabelValues:    bhead.labelValues,
			StartTimestamp: bhead.ts,
			Points:         points,
		})
	}

	head := bundlings[0]
	metric := &metricspb.Metric{
		Descriptor_: &metricspb.Metric_MetricDescriptor{
			MetricDescriptor: head.md,
		},
		Timeseries: timeseries,
	}

	return head.node, head.resource, metric
}

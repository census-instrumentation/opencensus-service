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

package tracetranslator

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"github.com/census-instrumentation/opencensus-service/internal/testutils"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/jaegertracing/jaeger/thrift-gen/jaeger"
)

func TestJaegerFromOCProtoTraceIDRoundTrip(t *testing.T) {
	wl := int64(0x0001020304050607)
	wh := int64(0x70605040302010FF)
	gl, gh, err := traceIDBytesToLowAndHigh(jTraceIDToOCProtoTraceID(wh, wl))
	if err != nil {
		t.Errorf("Error converting from OC trace id: %v", err)
	}
	if gl != wl || gh != wh {
		t.Errorf("Round trip of trace Id failed want: (0x%0x, 0x%0x) got: (0x%0x, 0x%0x)", wl, wh, gl, gh)
	}
}

func TestJaegerFromOCProtoSpanIDRoundTrip(t *testing.T) {
	w := int64(0x0001020304050607)
	g, err := ocIDBytesToJaegerID(jSpanIDToOCProtoSpanID(w))
	if err != nil {
		t.Errorf("Error converting from OC span id: %v", err)
	}
	if g != w {
		t.Errorf("Round trip of span Id failed want: 0x%0x got: 0x%0x", w, g)
	}
}

func TestOCProtoToJaegerThrift(t *testing.T) {
	const numOfFiles = 2
	for i := 0; i < numOfFiles; i++ {
		ocBatch := ocBatches[i]

		gotJBatch, err := OCProtoToJaegerThrift(ocBatch)
		if err != nil {
			t.Errorf("Failed to translate OC batch to Jaeger Thrift: %v", err)
			continue
		}

		wantSpanCount, gotSpanCount := len(ocBatch.Spans), len(gotJBatch.Spans)
		if wantSpanCount != gotSpanCount {
			t.Errorf("Different number of spans in the batches on pass #%d (want %d, got %d)", i, wantSpanCount, gotSpanCount)
			continue
		}

		// Jaeger binary tags do not round trip from Jaeger -> OCProto -> Jaeger.
		// For tests use data without binary tags.
		thriftFile := fmt.Sprintf("./testdata/thrift_batch_no_binary_tags_%02d.json", i+1)
		wantJBatch := &jaeger.Batch{}
		if err := loadFromJSON(thriftFile, wantJBatch); err != nil {
			t.Errorf("Failed load Jaeger Thrift from %q: %v", thriftFile, err)
			continue
		}

		// Sort tags to help with comparison, not only for jaeger.Process but also
		// on each span.
		sort.Slice(gotJBatch.Process.Tags, func(i, j int) bool {
			return gotJBatch.Process.Tags[i].Key < gotJBatch.Process.Tags[j].Key
		})
		sort.Slice(wantJBatch.Process.Tags, func(i, j int) bool {
			return wantJBatch.Process.Tags[i].Key < wantJBatch.Process.Tags[j].Key
		})
		var jSpans []*jaeger.Span
		jSpans = append(jSpans, gotJBatch.Spans...)
		jSpans = append(jSpans, wantJBatch.Spans...)
		for _, jSpan := range jSpans {
			sort.Slice(jSpan.Tags, func(i, j int) bool {
				return jSpan.Tags[i].Key < jSpan.Tags[j].Key
			})
		}

		gjson, _ := json.Marshal(gotJBatch)
		wjson, _ := json.Marshal(wantJBatch)
		gjsonStr := testutils.GenerateNormalizedJSON(string(gjson))
		wjsonStr := testutils.GenerateNormalizedJSON(string(wjson))
		if gjsonStr != wjsonStr {
			t.Errorf("OC Proto to Jaeger Thrift failed.\nGot:\n%s\nWant:\n%s\n", gjsonStr, wjsonStr)
		}
	}
}

// ocBatches has the OpenCensus proto batches used in the test. They are hard coded because
// structs like tracepb.AttributeMap cannot be ready from JSON.
var ocBatches = []*agenttracepb.ExportTraceServiceRequest{
	{
		Node: &commonpb.Node{
			Identifier: &commonpb.ProcessIdentifier{
				HostName:       "api246-sjc1",
				Pid:            13,
				StartTimestamp: &timestamp.Timestamp{Seconds: 1485467190, Nanos: 639875000},
			},
			LibraryInfo: &commonpb.LibraryInfo{ExporterVersion: "someVersion"},
			ServiceInfo: &commonpb.ServiceInfo{Name: "api"},
			Attributes: map[string]string{
				"a.binary": "AQIDBAMCAQ==",
				"a.bool":   "true",
				"a.double": "1234.56789",
				"a.long":   "123456789",
				"ip":       "10.53.69.61",
			},
		},
		Spans: []*tracepb.Span{
			{
				TraceId:      []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x52, 0x96, 0x9A, 0x89, 0x55, 0x57, 0x1A, 0x3F},
				SpanId:       []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x64, 0x7D, 0x98},
				ParentSpanId: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x68, 0xC4, 0xE3},
				Name:         &tracepb.TruncatableString{Value: "get"},
				Kind:         tracepb.Span_SERVER,
				StartTime:    &timestamp.Timestamp{Seconds: 1485467191, Nanos: 639875000},
				EndTime:      &timestamp.Timestamp{Seconds: 1485467191, Nanos: 662813000},
				Attributes: &tracepb.Span_Attributes{
					AttributeMap: map[string]*tracepb.AttributeValue{
						"http.url": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "http://127.0.0.1:15598/client_transactions"}},
						},
						"peer.ipv4": {
							Value: &tracepb.AttributeValue_IntValue{IntValue: 3224716605},
						},
						"peer.port": {
							Value: &tracepb.AttributeValue_IntValue{IntValue: 53931},
						},
						"peer.service": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "rtapi"}},
						},
						"someBool": {
							Value: &tracepb.AttributeValue_BoolValue{BoolValue: true},
						},
						"someDouble": {
							Value: &tracepb.AttributeValue_DoubleValue{DoubleValue: 129.8},
						},
					},
				},
				TimeEvents: &tracepb.Span_TimeEvents{
					TimeEvent: []*tracepb.Span_TimeEvent{
						{
							Time: &timestamp.Timestamp{Seconds: 1485467191, Nanos: 639875000},
							Value: &tracepb.Span_TimeEvent_Annotation_{
								Annotation: &tracepb.Span_TimeEvent_Annotation{
									Attributes: &tracepb.Span_Attributes{
										AttributeMap: map[string]*tracepb.AttributeValue{
											"key1": {
												Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "value1"}},
											},
										},
									},
								},
							},
						},
						{
							Time: &timestamp.Timestamp{Seconds: 1485467191, Nanos: 639875000},
							Value: &tracepb.Span_TimeEvent_Annotation_{
								Annotation: &tracepb.Span_TimeEvent_Annotation{
									Attributes: &tracepb.Span_Attributes{
										AttributeMap: map[string]*tracepb.AttributeValue{
											"event": {
												Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "nothing"}},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	},
	{
		Node: &commonpb.Node{
			ServiceInfo: &commonpb.ServiceInfo{Name: "api"},
		},
		Spans: []*tracepb.Span{
			{
				TraceId:      []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x52, 0x96, 0x9A, 0x89, 0x55, 0x57, 0x1A, 0x3F},
				SpanId:       []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x64, 0x7D, 0x98},
				ParentSpanId: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x68, 0xC4, 0xE3},
				Name:         &tracepb.TruncatableString{Value: "get"},
				Kind:         tracepb.Span_SERVER,
				StartTime:    &timestamp.Timestamp{Seconds: 1485467191, Nanos: 639875000},
				EndTime:      &timestamp.Timestamp{Seconds: 1485467191, Nanos: 662813000},
				Attributes: &tracepb.Span_Attributes{
					AttributeMap: map[string]*tracepb.AttributeValue{
						"peer.service": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "AAAAAAAAMDk="}},
						},
					},
				},
			},
			{
				TraceId:      []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x52, 0x96, 0x9A, 0x89, 0x55, 0x57, 0x1A, 0x3F},
				SpanId:       []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x64, 0x7D, 0x99},
				ParentSpanId: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x68, 0xC4, 0xE3},
				Name:         &tracepb.TruncatableString{Value: "get"},
				Kind:         tracepb.Span_SERVER,
				StartTime:    &timestamp.Timestamp{Seconds: 1485467191, Nanos: 639875000},
				EndTime:      &timestamp.Timestamp{Seconds: 1485467191, Nanos: 662813000},
				Links: &tracepb.Span_Links{
					Link: []*tracepb.Span_Link{
						{
							TraceId: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x52, 0x96, 0x9A, 0x89, 0x55, 0x57, 0x1A, 0x3F},
							SpanId:  []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x64, 0x7D, 0x98},
							Type:    tracepb.Span_Link_PARENT_LINKED_SPAN,
						},
						{
							TraceId: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x52, 0x96, 0x9A, 0x89, 0x55, 0x57, 0x1A, 0x3F},
							SpanId:  []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x68, 0xC4, 0xE3},
						},
					},
				},
			},
		},
	},
}

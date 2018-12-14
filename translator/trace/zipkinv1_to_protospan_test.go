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
	"io/ioutil"
	"reflect"
	"sort"
	"testing"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"github.com/golang/protobuf/ptypes/timestamp"
)

func Test_hexIDToOCID(t *testing.T) {
	tests := []struct {
		name    string
		hexStr  string
		want    []byte
		wantErr error
	}{
		{
			name:    "empty hex string",
			hexStr:  "",
			want:    nil,
			wantErr: errHexIDWrongLen,
		},
		{
			name:    "wrong length",
			hexStr:  "0000",
			want:    nil,
			wantErr: errHexIDWrongLen,
		},
		{
			name:    "parse error",
			hexStr:  "000000000000000-",
			want:    nil,
			wantErr: errHexIDParsing,
		},
		{
			name:    "all zero",
			hexStr:  "0000000000000000",
			want:    nil,
			wantErr: errHexIDZero,
		},
		{
			name:    "happy path",
			hexStr:  "0706050400010203",
			want:    []byte{7, 6, 5, 4, 0, 1, 2, 3},
			wantErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := hexIDToOCID(tt.hexStr)
			if tt.wantErr != nil && tt.wantErr != err {
				t.Errorf("hexIDToOCID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("hexIDToOCID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_hexTraceIDToOCTraceID(t *testing.T) {
	tests := []struct {
		name    string
		hexStr  string
		want    []byte
		wantErr error
	}{
		{
			name:    "empty hex string",
			hexStr:  "",
			want:    nil,
			wantErr: errHexTraceIDWrongLen,
		},
		{
			name:    "wrong length",
			hexStr:  "000000000000000010",
			want:    nil,
			wantErr: errHexTraceIDWrongLen,
		},
		{
			name:    "parse error",
			hexStr:  "000000000000000X0000000000000000",
			want:    nil,
			wantErr: errHexTraceIDParsing,
		},
		{
			name:    "all zero",
			hexStr:  "00000000000000000000000000000000",
			want:    nil,
			wantErr: errHexTraceIDZero,
		},
		{
			name:    "happy path",
			hexStr:  "00000000000000010000000000000002",
			want:    []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 2},
			wantErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := hexTraceIDToOCTraceID(tt.hexStr)
			if tt.wantErr != nil && tt.wantErr != err {
				t.Errorf("hexTraceIDToOCTraceID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("hexTraceIDToOCTraceID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSingleJSONZipkinV1BatchToOCProto(t *testing.T) {
	blob, err := ioutil.ReadFile("./testdata/zipkin_v1_single_batch.json")
	if err != nil {
		t.Fatalf("failed to load test data: %v", err)
	}
	got, err := ZipkinV1JSONBatchToOCProto(blob)
	if err != nil {
		t.Fatalf("failed to translate zipkinv1 to OC proto: %v", err)
	}

	want := ocBatchesFromZipkinV1
	sortTrace(want)
	sortTrace(got)

	if !reflect.DeepEqual(got, want) {
		saveAsFormattedJSON("./testdata/got.json", got)
		saveAsFormattedJSON("./testdata/want.json", want)
		t.Fatalf("got different data than want")
	}
}

func TestMultipleJSONZipkinV1BatchesToOCProto(t *testing.T) {
	blob, err := ioutil.ReadFile("./testdata/zipkin_v1_multiple_batches.json")
	if err != nil {
		t.Fatalf("failed to load test data: %v", err)
	}

	var batches []interface{}
	if err := json.Unmarshal(blob, &batches); err != nil {
		t.Fatalf("failed to load the batches: %v", err)
	}

	nodeToTraceReqs := make(map[string]*agenttracepb.ExportTraceServiceRequest)
	var got []*agenttracepb.ExportTraceServiceRequest
	for _, batch := range batches {
		jsonBatch, err := json.Marshal(batch)
		if err != nil {
			t.Fatalf("failed to marshal interface back to blob: %v", err)
		}

		g, err := ZipkinV1JSONBatchToOCProto(jsonBatch)
		if err != nil {
			t.Fatalf("failed to translate zipkinv1 to OC proto: %v", err)
		}

		// Coalesce the nodes otherwise they will differ due to multiple
		// nodes representing same logical service
		for _, tsr := range g {
			key := tsr.Node.String()
			if pTsr, ok := nodeToTraceReqs[key]; ok {
				pTsr.Spans = append(pTsr.Spans, tsr.Spans...)
			} else {
				nodeToTraceReqs[key] = tsr
			}
		}
	}

	for _, tsr := range nodeToTraceReqs {
		got = append(got, tsr)
	}

	want := ocBatchesFromZipkinV1
	sortTrace(want)
	sortTrace(got)

	if !reflect.DeepEqual(got, want) {
		saveAsFormattedJSON("./testdata/got.json", got)
		saveAsFormattedJSON("./testdata/want.json", want)
		t.Fatalf("got different data than want")
	}
}

func sortTrace(trace []*agenttracepb.ExportTraceServiceRequest) {
	sort.Slice(trace, func(i, j int) bool {
		return trace[i].Node.ServiceInfo.Name < trace[j].Node.ServiceInfo.Name
	})
}

// SaveAsFormattedJSON save the object as a formatted JSON file to help
// with investigations.
func saveAsFormattedJSON(file string, o interface{}) error {
	blob, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(file, blob, 0644)
}

// ocBatches has the OpenCensus proto batches used in the test. They are hard coded because
// structs like tracepb.AttributeMap cannot be ready from JSON.
var ocBatchesFromZipkinV1 = []*agenttracepb.ExportTraceServiceRequest{
	{
		Node: &commonpb.Node{
			ServiceInfo: &commonpb.ServiceInfo{Name: "front-proxy"},
			Attributes:  map[string]string{"ipv4": "172.31.0.2"},
		},
		Spans: []*tracepb.Span{
			{
				TraceId:      []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0e, 0xd2, 0xe6, 0x3c, 0xbe, 0x71, 0xf5, 0xa8},
				SpanId:       []byte{0x0e, 0xd2, 0xe6, 0x3c, 0xbe, 0x71, 0xf5, 0xa8},
				ParentSpanId: nil,
				Name:         &tracepb.TruncatableString{Value: "checkAvailability"},
				Kind:         tracepb.Span_CLIENT,
				// Status:       &tracepb.Status{Code: 0},
				StartTime: &timestamp.Timestamp{Seconds: 1544805927, Nanos: 446743000},
				EndTime:   &timestamp.Timestamp{Seconds: 1544805927, Nanos: 459699000},
				Attributes: &tracepb.Span_Attributes{
					AttributeMap: map[string]*tracepb.AttributeValue{
						"component": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "proxy"}},
						},
						"downstream_cluster": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "-"}},
						},
						"guid:x-request-id": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "5fe2c726-60c3-94b4-8581-d228ea36fd6d"}},
						},
						"http.method": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "GET"}},
						},
						"http.protocol": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "HTTP/1.1"}},
						},
						"http.status_code": {
							Value: &tracepb.AttributeValue_IntValue{IntValue: 200},
						},
						"http.url": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "http://0.0.0.0:8000/trace/1"}},
						},
						"request_size": {
							Value: &tracepb.AttributeValue_IntValue{IntValue: 0},
						},
						"response_flags": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "-"}},
						},
						"response_size": {
							Value: &tracepb.AttributeValue_IntValue{IntValue: 89},
						},
						"upstream_cluster": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "service1"}},
						},
						"user_agent": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "curl/7.54.0"}},
						},
					},
				},
				TimeEvents: &tracepb.Span_TimeEvents{
					TimeEvent: []*tracepb.Span_TimeEvent{
						{
							Time: &timestamp.Timestamp{Seconds: 1544805927, Nanos: 446743000},
							Value: &tracepb.Span_TimeEvent_Annotation_{
								Annotation: &tracepb.Span_TimeEvent_Annotation{
									Attributes: &tracepb.Span_Attributes{
										AttributeMap: map[string]*tracepb.AttributeValue{
											"cs": {
												Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "front-proxy"}},
											},
										},
									},
								},
							},
						},
						{
							Time: &timestamp.Timestamp{Seconds: 1544805927, Nanos: 460510000},
							Value: &tracepb.Span_TimeEvent_Annotation_{
								Annotation: &tracepb.Span_TimeEvent_Annotation{
									Attributes: &tracepb.Span_Attributes{
										AttributeMap: map[string]*tracepb.AttributeValue{
											"cr": {
												Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "front-proxy"}},
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
			ServiceInfo: &commonpb.ServiceInfo{Name: "service1"},
			Attributes:  map[string]string{"ipv4": "172.31.0.4"},
		},
		Spans: []*tracepb.Span{
			{
				TraceId:      []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0e, 0xd2, 0xe6, 0x3c, 0xbe, 0x71, 0xf5, 0xa8},
				SpanId:       []byte{0x0e, 0xd2, 0xe6, 0x3c, 0xbe, 0x71, 0xf5, 0xa8},
				ParentSpanId: nil,
				Name:         &tracepb.TruncatableString{Value: "checkAvailability"},
				Kind:         tracepb.Span_SERVER,
				// Status:       &tracepb.Status{Code: 0},
				StartTime: &timestamp.Timestamp{Seconds: 1544805927, Nanos: 448081000},
				EndTime:   &timestamp.Timestamp{Seconds: 1544805927, Nanos: 460102000},
				Attributes: &tracepb.Span_Attributes{
					AttributeMap: map[string]*tracepb.AttributeValue{
						"component": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "proxy"}},
						},
						"downstream_cluster": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "-"}},
						},
						"guid:x-request-id": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "5fe2c726-60c3-94b4-8581-d228ea36fd6d"}},
						},
						"http.method": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "GET"}},
						},
						"http.protocol": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "HTTP/2"}},
						},
						"http.status_code": {
							Value: &tracepb.AttributeValue_IntValue{IntValue: 200},
						},
						"http.url": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "http://0.0.0.0:8000/trace/1"}},
						},
						"request_size": {
							Value: &tracepb.AttributeValue_IntValue{IntValue: 0},
						},
						"response_flags": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "-"}},
						},
						"response_size": {
							Value: &tracepb.AttributeValue_IntValue{IntValue: 89},
						},
						"upstream_cluster": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "local_service"}},
						},
						"user_agent": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "curl/7.54.0"}},
						},
					},
				},
				TimeEvents: &tracepb.Span_TimeEvents{
					TimeEvent: []*tracepb.Span_TimeEvent{
						{
							Time: &timestamp.Timestamp{Seconds: 1544805927, Nanos: 448081000},
							Value: &tracepb.Span_TimeEvent_Annotation_{
								Annotation: &tracepb.Span_TimeEvent_Annotation{
									Attributes: &tracepb.Span_Attributes{
										AttributeMap: map[string]*tracepb.AttributeValue{
											"sr": {
												Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "service1"}},
											},
										},
									},
								},
							},
						},
						{
							Time: &timestamp.Timestamp{Seconds: 1544805927, Nanos: 460102000},
							Value: &tracepb.Span_TimeEvent_Annotation_{
								Annotation: &tracepb.Span_TimeEvent_Annotation{
									Attributes: &tracepb.Span_Attributes{
										AttributeMap: map[string]*tracepb.AttributeValue{
											"ss": {
												Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "service1"}},
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
				TraceId:      []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0e, 0xd2, 0xe6, 0x3c, 0xbe, 0x71, 0xf5, 0xa8},
				SpanId:       []byte{0xf9, 0xeb, 0xb6, 0xe6, 0x48, 0x80, 0x61, 0x2a},
				ParentSpanId: []byte{0x0e, 0xd2, 0xe6, 0x3c, 0xbe, 0x71, 0xf5, 0xa8},
				Name:         &tracepb.TruncatableString{Value: "checkStock"},
				Kind:         tracepb.Span_CLIENT,
				// Status:       &tracepb.Status{Code: 0},
				StartTime: &timestamp.Timestamp{Seconds: 1544805927, Nanos: 453923000},
				EndTime:   &timestamp.Timestamp{Seconds: 1544805927, Nanos: 457663000},
				Attributes: &tracepb.Span_Attributes{
					AttributeMap: map[string]*tracepb.AttributeValue{
						"component": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "proxy"}},
						},
						"downstream_cluster": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "-"}},
						},
						"guid:x-request-id": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "5fe2c726-60c3-94b4-8581-d228ea36fd6d"}},
						},
						"http.method": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "GET"}},
						},
						"http.protocol": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "HTTP/1.1"}},
						},
						"http.status_code": {
							Value: &tracepb.AttributeValue_IntValue{IntValue: 200},
						},
						"http.url": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "http://localhost:9000/trace/2"}},
						},
						"request_size": {
							Value: &tracepb.AttributeValue_IntValue{IntValue: 0},
						},
						"response_flags": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "-"}},
						},
						"response_size": {
							Value: &tracepb.AttributeValue_IntValue{IntValue: 89},
						},
						"upstream_cluster": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "service2"}},
						},
						"user_agent": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "python-requests/2.18.4"}},
						},
					},
				},
				TimeEvents: &tracepb.Span_TimeEvents{
					TimeEvent: []*tracepb.Span_TimeEvent{
						{
							Time: &timestamp.Timestamp{Seconds: 1544805927, Nanos: 453923000},
							Value: &tracepb.Span_TimeEvent_Annotation_{
								Annotation: &tracepb.Span_TimeEvent_Annotation{
									Attributes: &tracepb.Span_Attributes{
										AttributeMap: map[string]*tracepb.AttributeValue{
											"cs": {
												Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "service1"}},
											},
										},
									},
								},
							},
						},
						{
							Time: &timestamp.Timestamp{Seconds: 1544805927, Nanos: 457717000},
							Value: &tracepb.Span_TimeEvent_Annotation_{
								Annotation: &tracepb.Span_TimeEvent_Annotation{
									Attributes: &tracepb.Span_Attributes{
										AttributeMap: map[string]*tracepb.AttributeValue{
											"cr": {
												Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "service1"}},
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
			ServiceInfo: &commonpb.ServiceInfo{Name: "service2"},
			Attributes:  map[string]string{"ipv4": "172.31.0.7"},
		},
		Spans: []*tracepb.Span{
			{
				TraceId:      []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0e, 0xd2, 0xe6, 0x3c, 0xbe, 0x71, 0xf5, 0xa8},
				SpanId:       []byte{0xf9, 0xeb, 0xb6, 0xe6, 0x48, 0x80, 0x61, 0x2a},
				ParentSpanId: []byte{0x0e, 0xd2, 0xe6, 0x3c, 0xbe, 0x71, 0xf5, 0xa8},
				Name:         &tracepb.TruncatableString{Value: "checkStock"},
				Kind:         tracepb.Span_SERVER,
				// Status:       &tracepb.Status{Code: 0},
				StartTime: &timestamp.Timestamp{Seconds: 1544805927, Nanos: 454487000},
				EndTime:   &timestamp.Timestamp{Seconds: 1544805927, Nanos: 457320000},
				Attributes: &tracepb.Span_Attributes{
					AttributeMap: map[string]*tracepb.AttributeValue{
						"component": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "proxy"}},
						},
						"downstream_cluster": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "-"}},
						},
						"guid:x-request-id": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "5fe2c726-60c3-94b4-8581-d228ea36fd6d"}},
						},
						"http.method": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "GET"}},
						},
						"http.protocol": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "HTTP/2"}},
						},
						"http.status_code": {
							Value: &tracepb.AttributeValue_IntValue{IntValue: 200},
						},
						"http.url": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "http://localhost:9000/trace/2"}},
						},
						"request_size": {
							Value: &tracepb.AttributeValue_IntValue{IntValue: 0},
						},
						"response_flags": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "-"}},
						},
						"response_size": {
							Value: &tracepb.AttributeValue_IntValue{IntValue: 89},
						},
						"upstream_cluster": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "local_service"}},
						},
						"user_agent": {
							Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "python-requests/2.18.4"}},
						},
					},
				},
				TimeEvents: &tracepb.Span_TimeEvents{
					TimeEvent: []*tracepb.Span_TimeEvent{
						{
							Time: &timestamp.Timestamp{Seconds: 1544805927, Nanos: 454487000},
							Value: &tracepb.Span_TimeEvent_Annotation_{
								Annotation: &tracepb.Span_TimeEvent_Annotation{
									Attributes: &tracepb.Span_Attributes{
										AttributeMap: map[string]*tracepb.AttributeValue{
											"sr": {
												Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "service2"}},
											},
										},
									},
								},
							},
						},
						{
							Time: &timestamp.Timestamp{Seconds: 1544805927, Nanos: 457320000},
							Value: &tracepb.Span_TimeEvent_Annotation_{
								Annotation: &tracepb.Span_TimeEvent_Annotation{
									Attributes: &tracepb.Span_Attributes{
										AttributeMap: map[string]*tracepb.AttributeValue{
											"ss": {
												Value: &tracepb.AttributeValue_StringValue{StringValue: &tracepb.TruncatableString{Value: "service2"}},
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
}

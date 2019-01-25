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

package tracetranslator

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"

	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/jaegertracing/jaeger/thrift-gen/zipkincore"
)

// ZipkinV1ThriftBatchToOCProto converts Zipkin v1 spans to OC Proto.
func ZipkinV1ThriftBatchToOCProto(zSpans []*zipkincore.Span) ([]*agenttracepb.ExportTraceServiceRequest, error) {
	return zipkinToOCProto(func(i int) (*tracepb.Span, *annotationParseResult, error) {
		if i >= len(zSpans) {
			return nil, nil, errNoMoreSpans
		}
		return zipkinV1ThriftToOCSpan(zSpans[i])
	})
}

func zipkinV1ThriftToOCSpan(zSpan *zipkincore.Span) (*tracepb.Span, *annotationParseResult, error) {
	traceIDHigh := int64(0)
	if zSpan.TraceIDHigh != nil {
		traceIDHigh = *zSpan.TraceIDHigh
	}

	// TODO: (@pjanotti) ideally we should error here instead of generating invalid OC proto
	// however per https://github.com/census-instrumentation/opencensus-service/issues/349
	// failures on the receivers in general are silent at this moment, so letting them
	// proceed for now. We should validate the traceID, spanID and parentID are good with
	// OC proto requirements.
	traceID := traceIDToOCProtoTraceID(traceIDHigh, zSpan.TraceID)
	spanID := spanIDToOCProtoSpanID(zSpan.ID)
	var parentID []byte
	if zSpan.ParentID != nil {
		id := spanIDToOCProtoSpanID(*zSpan.ParentID)
		parentID = id
	}

	parsedAnnotations := parseZipkinV1ThriftAnnotations(zSpan.Annotations)
	attributes, localComponent := zipkinV1ThriftBinAnnotationsToOCAttributes(zSpan.BinaryAnnotations)
	if parsedAnnotations.Endpoint.ServiceName == unknownServiceName && localComponent != "" {
		parsedAnnotations.Endpoint.ServiceName = localComponent
	}

	var startTime, endTime *timestamp.Timestamp
	if zSpan.Timestamp == nil {
		startTime = parsedAnnotations.EarlyAnnotationTime
		endTime = parsedAnnotations.LateAnnotationTime
	} else {
		startTime = epochMicrosecondsToTimestamp(*zSpan.Timestamp)
		var duration int64
		if zSpan.Duration != nil {
			duration = *zSpan.Duration
		}
		endTime = epochMicrosecondsToTimestamp(*zSpan.Timestamp + duration)
	}

	ocSpan := &tracepb.Span{
		TraceId:      traceID,
		SpanId:       spanID,
		ParentSpanId: parentID,
		Kind:         parsedAnnotations.Kind,
		TimeEvents:   parsedAnnotations.TimeEvents,
		StartTime:    startTime,
		EndTime:      endTime,
		Attributes:   attributes,
	}

	if zSpan.Name != "" {
		ocSpan.Name = &tracepb.TruncatableString{Value: zSpan.Name}
	}

	return ocSpan, parsedAnnotations, nil
}

func parseZipkinV1ThriftAnnotations(ztAnnotations []*zipkincore.Annotation) *annotationParseResult {
	annotations := make([]*annotation, 0, len(ztAnnotations))
	for _, ztAnnot := range ztAnnotations {
		annot := &annotation{
			Timestamp: ztAnnot.Timestamp,
			Value:     ztAnnot.Value,
			Endpoint:  toTranslatorEndpoint(ztAnnot.Host),
		}
		annotations = append(annotations, annot)
	}
	return parseZipkinV1Annotations(annotations)
}

func toTranslatorEndpoint(e *zipkincore.Endpoint) *endpoint {
	if e == nil {
		return nil
	}

	var ipv4, ipv6 string
	if e.Ipv4 != 0 {
		ipv4 = net.IPv4(byte(e.Ipv4>>24), byte(e.Ipv4>>16), byte(e.Ipv4>>8), byte(e.Ipv4)).String()
	}
	if len(e.Ipv6) != 0 {
		ipv6 = net.IP(e.Ipv6).String()
	}
	return &endpoint{
		ServiceName: e.ServiceName,
		IPv4:        ipv4,
		IPv6:        ipv6,
		Port:        int32(e.Port),
	}
}

var trueByteSlice = []byte{1}

func zipkinV1ThriftBinAnnotationsToOCAttributes(ztBinAnnotations []*zipkincore.BinaryAnnotation) (attributes *tracepb.Span_Attributes, localComponent string) {
	if len(ztBinAnnotations) == 0 {
		return nil, ""
	}

	attributeMap := make(map[string]*tracepb.AttributeValue)
	for _, binaryAnnotation := range ztBinAnnotations {
		pbAttrib := &tracepb.AttributeValue{}
		binAnnotationType := binaryAnnotation.AnnotationType
		switch binaryAnnotation.AnnotationType {
		case zipkincore.AnnotationType_BOOL:
			vBool := bytes.Equal(binaryAnnotation.Value, trueByteSlice)
			pbAttrib.Value = &tracepb.AttributeValue_BoolValue{BoolValue: vBool}
		case zipkincore.AnnotationType_BYTES:
			bytesStr := base64.StdEncoding.EncodeToString(binaryAnnotation.Value)
			pbAttrib.Value = &tracepb.AttributeValue_StringValue{
				StringValue: &tracepb.TruncatableString{Value: bytesStr}}
		case zipkincore.AnnotationType_DOUBLE:
			var d float64
			if err := bytesToNumber(binaryAnnotation.Value, &d); err != nil {
				pbAttrib.Value = strAttributeForError(err)
			} else {
				pbAttrib.Value = &tracepb.AttributeValue_DoubleValue{DoubleValue: d}
			}
		case zipkincore.AnnotationType_I16:
			var i int16
			if err := bytesToNumber(binaryAnnotation.Value, &i); err != nil {
				pbAttrib.Value = strAttributeForError(err)
			} else {
				pbAttrib.Value = &tracepb.AttributeValue_IntValue{IntValue: int64(i)}
			}
		case zipkincore.AnnotationType_I32:
			var i int32
			if err := bytesToNumber(binaryAnnotation.Value, &i); err != nil {
				pbAttrib.Value = strAttributeForError(err)
			} else {
				pbAttrib.Value = &tracepb.AttributeValue_IntValue{IntValue: int64(i)}
			}
		case zipkincore.AnnotationType_I64:
			var i int64
			if err := bytesToNumber(binaryAnnotation.Value, &i); err != nil {
				pbAttrib.Value = strAttributeForError(err)
			} else {
				pbAttrib.Value = &tracepb.AttributeValue_IntValue{IntValue: i}
			}
		case zipkincore.AnnotationType_STRING:
			pbAttrib.Value = &tracepb.AttributeValue_StringValue{
				StringValue: &tracepb.TruncatableString{Value: string(binaryAnnotation.Value)}}
		default:
			err := fmt.Errorf("unknown zipkin v1 binary annotation type (%d)", int(binAnnotationType))
			pbAttrib.Value = strAttributeForError(err)
		}

		key := binaryAnnotation.Key
		if key == zipkincore.LOCAL_COMPONENT {
			// TODO: (@pjanotti) add reference to OpenTracing and change related tags to use them
			key = "component"
			localComponent = string(binaryAnnotation.Value)
		}

		attributeMap[key] = pbAttrib
	}

	attributes = &tracepb.Span_Attributes{
		AttributeMap: attributeMap,
	}
	return attributes, localComponent
}

func bytesToNumber(b []byte, number interface{}) error {
	buf := bytes.NewReader(b)
	return binary.Read(buf, binary.BigEndian, number)
}

func strAttributeForError(err error) *tracepb.AttributeValue_StringValue {
	return &tracepb.AttributeValue_StringValue{
		StringValue: &tracepb.TruncatableString{
			Value: "<" + err.Error() + ">",
		},
	}
}

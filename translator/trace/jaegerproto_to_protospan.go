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
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	jmodel "github.com/jaegertracing/jaeger/model"

	"github.com/census-instrumentation/opencensus-service/internal"
)

// JaegerProtoToOCProtoBatch converts a slice of jaeger gRPC protobuf spans into oc ExportTraceServiceRequest
// batches. Spans following a span with a set process are assumed to be from the same process (node).
func JaegerProtoToOCProtoBatch(jSpans []*jmodel.Span) ([]*agenttracepb.ExportTraceServiceRequest, error) {
	ocSpans := make([]*tracepb.Span, 0, len(jSpans))
	var ocNode *commonpb.Node
	ocBatches := make([]*agenttracepb.ExportTraceServiceRequest, 0, len(jSpans))
	for spanIndex, span := range jSpans {
		ocSpan, err := JaegerProtoToOCProto(span)
		if err != nil {
			return nil, err
		}
		ocSpans := append(ocSpans, ocSpan)
		if span.Process != nil {
			newOcNode := jProtoProcessToOCProtoNode(span.Process)
			if ocNode != nil && newOcNode != nil {
				// Cut new batch with old process, and reset to add spans to a batch for the new process
				ocBatch := &agenttracepb.ExportTraceServiceRequest{
					Node:  ocNode,
					Spans: ocSpans,
				}
				ocBatches = append(ocBatches, ocBatch)
				ocNode = newOcNode
				ocSpans = make([]*tracepb.Span, 0, len(jSpans)-spanIndex)
			}
		}
	}

	if len(ocSpans) > 0 {
		ocBatch := &agenttracepb.ExportTraceServiceRequest{
			Node:  ocNode,
			Spans: ocSpans,
		}
		ocBatches = append(ocBatches, ocBatch)
	}

	return ocBatches, nil
}

// JaegerProtoToOCProto converts a jaeger gRPC protobuf span into a oc Span
func JaegerProtoToOCProto(jSpan *jmodel.Span) (*tracepb.Span, error) {
	_, kind, status, attributes := jKeyValuesToAttributes(jSpan.Tags)

	traceID := make([]byte, 16)
	jSpan.TraceID.MarshalTo(traceID)

	spanID := make([]byte, 8)
	jSpan.SpanID.MarshalTo(spanID)

	parentSpanID := make([]byte, 8)
	jParentSpan := jSpan.ParentSpanID()
	jParentSpan.MarshalTo(parentSpanID)

	var name *tracepb.TruncatableString
	if jSpan.OperationName != "" {
		name = &tracepb.TruncatableString{Value: jSpan.OperationName}
	}

	return &tracepb.Span{
		TraceId:      traceID,
		SpanId:       spanID,
		ParentSpanId: parentSpanID,
		Name:         name,
		Kind:         kind,
		StartTime:    internal.TimeToTimestamp(jSpan.StartTime),
		EndTime:      internal.TimeToTimestamp(jSpan.StartTime.Add(time.Duration(jSpan.Duration) * time.Microsecond)),
		Attributes:   attributes,
		// TODO: StackTrace: OpenTracing defines a semantic key for "stack", should we attempt to its content to StackTrace?
		TimeEvents: jProtoLogsToOCProtoTimeEvents(jSpan.Logs),
		Links:      jProtoRefsToOCProtoLinks(jSpan.References),
		Status:     status,
	}, nil
}

func jProtoProcessToOCProtoNode(jProcess *jmodel.Process) *commonpb.Node {
	if jProcess == nil {
		return nil
	}

	node := &commonpb.Node{
		Identifier:  &commonpb.ProcessIdentifier{},
		LibraryInfo: &commonpb.LibraryInfo{},
		ServiceInfo: &commonpb.ServiceInfo{Name: jProcess.GetServiceName()},
	}
	attributes := make(map[string]string)
	for _, tag := range jProcess.GetTags() {
		// Special treatment for special keys in the tags.
		switch tag.Key {
		case "hostname":
			node.Identifier.HostName = tag.GetVStr()
			continue
		case "jaeger.version":
			node.LibraryInfo.ExporterVersion = "Jaeger-" + tag.GetVStr()
			continue
		}

		switch tag.GetVType() {
		case jmodel.ValueType_STRING:
			attributes[tag.Key] = tag.GetVStr()
		case jmodel.ValueType_BOOL:
			attributes[tag.Key] = strconv.FormatBool(tag.GetVBool())
		case jmodel.ValueType_INT64:
			attributes[tag.Key] = strconv.FormatInt(tag.GetVInt64(), 10)
		case jmodel.ValueType_FLOAT64:
			attributes[tag.Key] = strconv.FormatFloat(tag.GetVFloat64(), 'f', -1, 64)
		case jmodel.ValueType_BINARY:
			attributes[tag.Key] = base64.StdEncoding.EncodeToString(tag.GetVBinary())
		default:
			attributes[tag.Key] = fmt.Sprintf("<Unknown Jaeger TagType %q>", tag.GetVType())
		}
	}

	if len(attributes) > 0 {
		node.Attributes = attributes
	}
	return node
}

func jKeyValuesToAttributes(
	kvs jmodel.KeyValues,
) (string, tracepb.Span_SpanKind, *tracepb.Status, *tracepb.Span_Attributes) {
	if kvs == nil {
		return "", tracepb.Span_SPAN_KIND_UNSPECIFIED, nil, nil
	}

	// Init all special attributes
	var kind tracepb.Span_SpanKind
	var statusCodePtr *int32
	var statusMessage string
	var message string

	attributes := make(map[string]*tracepb.AttributeValue)

	for _, kv := range kvs {
		// First try to populate special opentracing defined tags from jaeger keyvalues.
		switch kv.Key {
		case OpentracingKeySpanKind:
			switch kv.GetVStr() {
			case "client":
				kind = tracepb.Span_CLIENT
			case "server":
				kind = tracepb.Span_SERVER
			}
		case OpentracingKeyHTTPStatusCode, OpentracingKeyStatusCode:
			// It is expected to be an int
			statusCodePtr = new(int32)
			*statusCodePtr = int32(kv.GetVInt64())
		case OpentracingKeyHTTPStatusMessage, OpentracingKeyStatusMessage:
			statusMessage = kv.GetVStr()
		case OpentracingKeyMessage:
			message = kv.GetVStr()
		}

		// Next, convert the keyvalue to an oc Span_Attribute.
		attrib := &tracepb.AttributeValue{}
		switch kv.VType {
		case jmodel.ValueType_STRING:
			attrib.Value = &tracepb.AttributeValue_StringValue{
				StringValue: &tracepb.TruncatableString{Value: kv.GetVStr()},
			}
		case jmodel.ValueType_BOOL:
			attrib.Value = &tracepb.AttributeValue_BoolValue{
				BoolValue: kv.GetVBool(),
			}
		case jmodel.ValueType_INT64:
			attrib.Value = &tracepb.AttributeValue_IntValue{
				IntValue: kv.GetVInt64(),
			}
		case jmodel.ValueType_FLOAT64:
			attrib.Value = &tracepb.AttributeValue_DoubleValue{
				DoubleValue: kv.GetVFloat64(),
			}
		case jmodel.ValueType_BINARY:
			attrib.Value = &tracepb.AttributeValue_StringValue{
				StringValue: &tracepb.TruncatableString{
					Value: base64.StdEncoding.EncodeToString(kv.GetVBinary()),
				},
			}
		default:
			attrib.Value = &tracepb.AttributeValue_StringValue{
				StringValue: &tracepb.TruncatableString{
					Value: fmt.Sprintf("<Unknown Jaeger ValueType %q>", kv.GetVType()),
				},
			}
		}
		attributes[kv.Key] = attrib
	}

	var status *tracepb.Status
	if statusCodePtr != nil || statusMessage != "" {
		statusCode := int32(0)
		if statusCodePtr != nil {
			statusCode = *statusCodePtr
		}
		status = &tracepb.Status{Message: statusMessage, Code: statusCode}
	}

	var spanAttributes *tracepb.Span_Attributes
	if len(attributes) > 0 {
		spanAttributes = &tracepb.Span_Attributes{AttributeMap: attributes}
	}

	return message, kind, status, spanAttributes
}

func jProtoLogsToOCProtoTimeEvents(logs []jmodel.Log) *tracepb.Span_TimeEvents {
	if logs == nil {
		return nil
	}

	timeEvents := make([]*tracepb.Span_TimeEvent, 0, len(logs))

	for _, log := range logs {
		description, _, _, attributes := jKeyValuesToAttributes(log.Fields)
		var annotation *tracepb.Span_TimeEvent_Annotation
		if attributes != nil {
			annotation = &tracepb.Span_TimeEvent_Annotation{
				Description: strToTruncatableString(description),
				Attributes:  attributes,
			}
		}
		timeEvent := &tracepb.Span_TimeEvent{
			Time:  internal.TimeToTimestamp(log.Timestamp),
			Value: &tracepb.Span_TimeEvent_Annotation_{Annotation: annotation},
		}

		timeEvents = append(timeEvents, timeEvent)
	}

	return &tracepb.Span_TimeEvents{TimeEvent: timeEvents}
}

func jProtoRefsToOCProtoLinks(jRefs []jmodel.SpanRef) *tracepb.Span_Links {
	if jRefs == nil {
		return nil
	}

	links := make([]*tracepb.Span_Link, 0, len(jRefs))

	for _, jRef := range jRefs {
		var linkType tracepb.Span_Link_Type
		if jRef.RefType == jmodel.SpanRefType_CHILD_OF {
			// Wording on OC for Span_Link_PARENT_LINKED_SPAN: The linked span is a parent of the current span.
			linkType = tracepb.Span_Link_PARENT_LINKED_SPAN
		} else {
			// TODO: SpanRefType_FOLLOWS_FROM doesn't map well to OC, so treat all other cases as unknown
			linkType = tracepb.Span_Link_TYPE_UNSPECIFIED
		}

		traceID := make([]byte, 16)
		jRef.TraceID.MarshalTo(traceID)

		spanID := make([]byte, 8)
		jRef.SpanID.MarshalTo(spanID)

		link := &tracepb.Span_Link{
			TraceId: traceID,
			SpanId:  spanID,
			Type:    linkType,
		}
		links = append(links, link)
	}
	return &tracepb.Span_Links{Link: links}
}

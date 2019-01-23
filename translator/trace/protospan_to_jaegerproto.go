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
	"encoding/binary"
	"fmt"

	"github.com/golang/protobuf/ptypes"
	jaeger "github.com/jaegertracing/jaeger/model"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
)

var (
	unknownJaegerProtoProcess = jaeger.Process{ServiceName: "unknown-service-name"}
)

// OCProtoToJaegerProto translates OpenCensus trace data into the Jaeger Protobuf format.
func OCProtoToJaegerProto(ocBatch *agenttracepb.ExportTraceServiceRequest) (*jaeger.Batch, error) {
	if ocBatch == nil {
		return nil, nil
	}

	jProcess := ocNodeToJaegerProtoProcess(ocBatch.Node)
	jSpans, err := ocSpansToJaegerProtoSpans(ocBatch.Spans, jProcess)
	if err != nil {
		return nil, err
	}

	jb := &jaeger.Batch{
		Process: jProcess,
		Spans:   jSpans,
	}

	return jb, nil
}

func ocNodeToJaegerProtoProcess(node *commonpb.Node) jaeger.Process {
	if node == nil {
		return unknownJaegerProtoProcess
	}

	// var jTags []jaeger.KeyValue
	nodeAttribsLen := len(node.Attributes)
	jTags := make([]jaeger.KeyValue, 0, nodeAttribsLen+6)
	if nodeAttribsLen > 0 {
		// jTags = make([]jaeger.KeyValue, 0, nodeAttribsLen)
		for k, v := range node.Attributes {
			str := v
			jTag := jaeger.KeyValue{
				Key:   k,
				VType: jaeger.ValueType_STRING,
				VStr:  str,
			}
			jTags = append(jTags, jTag)
		}
	}

	if node.Identifier != nil {
		if node.Identifier.HostName != "" {
			hostTag := jaeger.KeyValue{
				Key:   "hostname",
				VType: jaeger.ValueType_STRING,
				VStr:  node.Identifier.HostName,
			}
			jTags = append(jTags, hostTag)
		}
		if node.Identifier.Pid != 0 {
			pid := int64(node.Identifier.Pid)
			hostTag := jaeger.KeyValue{
				Key:    "pid",
				VType:  jaeger.ValueType_INT64,
				VInt64: pid,
			}
			jTags = append(jTags, hostTag)
		}
		if node.Identifier.StartTimestamp != nil && node.Identifier.StartTimestamp.Seconds != 0 {
			startTimeStr := ptypes.TimestampString(node.Identifier.StartTimestamp)
			hostTag := jaeger.KeyValue{
				Key:   "start.time",
				VType: jaeger.ValueType_STRING,
				VStr:  startTimeStr,
			}
			jTags = append(jTags, hostTag)
		}
	}

	// Add OpenCensus library information as tags if available
	ocLib := node.LibraryInfo
	if ocLib != nil {
		// Only add language if specified
		if ocLib.Language != commonpb.LibraryInfo_LANGUAGE_UNSPECIFIED {
			languageStr := ocLib.Language.String()
			languageTag := jaeger.KeyValue{
				Key:   "opencensus.language",
				VType: jaeger.ValueType_STRING,
				VStr:  languageStr,
			}
			jTags = append(jTags, languageTag)
		}
		if ocLib.ExporterVersion != "" {
			exporterTag := jaeger.KeyValue{
				Key:   "opencensus.exporterversion",
				VType: jaeger.ValueType_STRING,
				VStr:  ocLib.ExporterVersion,
			}
			jTags = append(jTags, exporterTag)
		}
		if ocLib.CoreLibraryVersion != "" {
			exporterTag := jaeger.KeyValue{
				Key:   "opencensus.corelibversion",
				VType: jaeger.ValueType_STRING,
				VStr:  ocLib.CoreLibraryVersion,
			}
			jTags = append(jTags, exporterTag)
		}
	}

	var serviceName string
	if node.ServiceInfo != nil && node.ServiceInfo.Name != "" {
		serviceName = node.ServiceInfo.Name
	}

	if serviceName == "" && len(jTags) == 0 {
		// No info to put in the process...
		return unknownJaegerProtoProcess
	}

	return jaeger.Process{
		ServiceName: serviceName,
		Tags:        jTags,
	}
}

func ocSpansToJaegerProtoSpans(ocSpans []*tracepb.Span, jProcess jaeger.Process) ([]*jaeger.Span, error) {
	if ocSpans == nil {
		return nil, nil
	}

	// Pre-allocate assuming that few, if any spans, are nil.
	jSpans := make([]*jaeger.Span, 0, len(ocSpans))
	for _, ocSpan := range ocSpans {
		// traceIDLow, traceIDHigh, err := traceIDBytesToLowAndHigh(ocSpan.TraceId)
		traceID, err := ocTraceIDToJaegerProtoTraceID(ocSpan.TraceId)
		if err != nil {
			return nil, fmt.Errorf("OC span has invalid trace ID: %v", err)
		}
		if traceID.High == 0 && traceID.Low == 0 {
			return nil, errZeroTraceID
		}
		jReferences, err := ocLinksToJaegerProtoReferences(ocSpan.Links)
		if err != nil {
			return nil, fmt.Errorf("Error converting OC links to Jaeger references: %v", err)
		}
		spanID, err := ocSpanIDToJaegerProtoSpanID(ocSpan.SpanId)
		if err != nil {
			return nil, fmt.Errorf("OC span has invalid span ID: %v", err)
		}
		if spanID == 0 {
			return nil, errZeroSpanID
		}
		// OC ParentSpanId can be nil/empty: only attempt conversion if not nil/empty.
		// TODO(owais): REVIEW: Do we need to convert parent ID to a span reference of child type
		// if a child type ref does not already exist??
		/*
			var parentSpanID int64
			if len(ocSpan.ParentSpanId) != 0 {
				parentSpanID, err = ocSpanIDToJaegerProtoSpanID(ocSpan.ParentSpanId)
				if err != nil {
					return nil, fmt.Errorf("OC span has invalid parent span ID: %v", err)
				}
			}
		*/
		startTime, err := ptypes.Timestamp(ocSpan.StartTime)
		if err != nil {
			// TODO: handle err
		}
		endTime, err := ptypes.Timestamp(ocSpan.EndTime)
		if err != nil {
			// TODO: handle err
		}
		jSpan := &jaeger.Span{
			TraceID:       traceID,
			SpanID:        spanID,
			OperationName: truncableStringToStr(ocSpan.Name),
			References:    jReferences,
			// Flags: TODO (@pjanotti) Nothing from OC-Proto seems to match the values for Flags see https://www.jaegertracing.io/docs/1.8/client-libraries/
			StartTime: startTime,
			Duration:  endTime.Sub(startTime),
			Tags:      ocSpanAttributesToJaegerProtoTags(ocSpan.Attributes),
			Logs:      ocTimeEventsToJaegerProtoLogs(ocSpan.TimeEvents),
			Process:   &jProcess,
		}

		if ocSpan.Attributes == nil {
			jSpan.Tags = appendJaegerProtoTagFromOCSpanKind(jSpan.Tags, ocSpan.Kind)
		} else {
			if _, ok := ocSpan.Attributes.AttributeMap["span.kind"]; !ok {
				jSpan.Tags = appendJaegerProtoTagFromOCSpanKind(jSpan.Tags, ocSpan.Kind)
			}
		}
		jSpans = append(jSpans, jSpan)
	}

	return jSpans, nil
}

func ocLinksToJaegerProtoReferences(ocSpanLinks *tracepb.Span_Links) ([]jaeger.SpanRef, error) {
	if ocSpanLinks == nil || ocSpanLinks.Link == nil {
		return nil, nil
	}

	ocLinks := ocSpanLinks.Link
	jRefs := make([]jaeger.SpanRef, 0, len(ocLinks))
	for _, ocLink := range ocLinks {
		traceID, err := ocTraceIDToJaegerProtoTraceID(ocLink.TraceId)
		if err != nil {
			return nil, fmt.Errorf("OC link has invalid trace ID: %v", err)
		}

		var jRefType jaeger.SpanRefType
		switch ocLink.Type {
		case tracepb.Span_Link_PARENT_LINKED_SPAN:
			jRefType = jaeger.SpanRefType_CHILD_OF
		default:
			// TODO: (@pjanotti) Jaeger doesn't have a unknown SpanRefType, it has FOLLOWS_FROM or CHILD_OF
			// at first mapping all others to FOLLOWS_FROM.
			jRefType = jaeger.SpanRefType_FOLLOWS_FROM
		}

		spanID, err := ocSpanIDToJaegerProtoSpanID(ocLink.SpanId)
		if err != nil {
			return nil, fmt.Errorf("OC link has invalid span ID: %v", err)
		}

		jRef := jaeger.SpanRef{
			TraceID: traceID,
			SpanID:  spanID,
			RefType: jRefType,
		}
		jRefs = append(jRefs, jRef)
	}

	return jRefs, nil
}

func appendJaegerProtoTagFromOCSpanKind(jTags []jaeger.KeyValue, ocSpanKind tracepb.Span_SpanKind) []jaeger.KeyValue {
	// TODO: (@pjanotti): Replace any OpenTracing literals by importing github.com/opentracing/opentracing-go/ext?
	var tagValue string
	switch ocSpanKind {
	case tracepb.Span_CLIENT:
		tagValue = "client"
	case tracepb.Span_SERVER:
		tagValue = "server"
	}

	if tagValue != "" {
		jTag := jaeger.KeyValue{
			Key:  "span.kind",
			VStr: tagValue,
		}
		jTags = append(jTags, jTag)
	}

	return jTags
}

func ocTimeEventsToJaegerProtoLogs(ocSpanTimeEvents *tracepb.Span_TimeEvents) []jaeger.Log {
	if ocSpanTimeEvents == nil || ocSpanTimeEvents.TimeEvent == nil {
		return make([]jaeger.Log, 0, 0)
	}

	ocTimeEvents := ocSpanTimeEvents.TimeEvent

	// Assume that in general no time events are going to produce nil Jaeger logs.
	jLogs := make([]jaeger.Log, 0, len(ocTimeEvents))
	for _, ocTimeEvent := range ocTimeEvents {
		ts, err := ptypes.Timestamp(ocTimeEvent.Time)
		if err != nil {
			// TODO: handler error
		}
		jLog := jaeger.Log{Timestamp: ts}
		switch teValue := ocTimeEvent.Value.(type) {
		case *tracepb.Span_TimeEvent_Annotation_:
			jLog.Fields = ocAnnotationToJagerProtoTags(teValue.Annotation)
		case *tracepb.Span_TimeEvent_MessageEvent_:
			jLog.Fields = ocMessageEventToJaegerProtoTags(teValue.MessageEvent)
		default:
			msg := "An unknown OpenCensus TimeEvent type was detected when translating to Jaeger"
			jTag := jaeger.KeyValue{
				Key:  "unknown.oc.timeevent.type",
				VStr: msg,
			}
			jLog.Fields = append(jLog.Fields, jTag)
		}

		jLogs = append(jLogs, jLog)
	}

	return jLogs
}

func ocAnnotationToJagerProtoTags(annotation *tracepb.Span_TimeEvent_Annotation) []jaeger.KeyValue {
	if annotation == nil {
		return make([]jaeger.KeyValue, 0, 0)
	}

	jTags := ocSpanAttributesToJaegerProtoTags(annotation.Attributes)

	desc := truncableStringToStr(annotation.Description)
	if desc != "" {
		jDescTag := jaeger.KeyValue{
			Key:   annotationDescriptionKey,
			VStr:  desc,
			VType: jaeger.ValueType_STRING,
		}
		jTags = append(jTags, jDescTag)
	}

	return jTags
}

func ocMessageEventToJaegerProtoTags(msgEvent *tracepb.Span_TimeEvent_MessageEvent) []jaeger.KeyValue {
	if msgEvent == nil {
		return make([]jaeger.KeyValue, 0, 0)
	}

	jID := int64(msgEvent.Id)
	idTag := jaeger.KeyValue{
		Key:    messageEventIDKey,
		VInt64: jID,
		VType:  jaeger.ValueType_INT64,
	}

	msgTypeStr := msgEvent.Type.String()
	msgType := jaeger.KeyValue{
		Key:   messageEventTypeKey,
		VStr:  msgTypeStr,
		VType: jaeger.ValueType_STRING,
	}

	// Some implementations always have these two fields as zeros.
	if msgEvent.CompressedSize == 0 && msgEvent.UncompressedSize == 0 {
		return []jaeger.KeyValue{
			idTag, msgType,
		}
	}

	// There is a risk in this cast since we are converting from uint64, but
	// seems a good compromise since the risk of such large values are small.
	compSize := int64(msgEvent.CompressedSize)
	compressedSize := jaeger.KeyValue{
		Key:    messageEventCompressedSizeKey,
		VInt64: compSize,
		VType:  jaeger.ValueType_INT64,
	}

	uncompSize := int64(msgEvent.UncompressedSize)
	uncompressedSize := jaeger.KeyValue{
		Key:    messageEventUncompressedSizeKey,
		VInt64: uncompSize,
		VType:  jaeger.ValueType_INT64,
	}

	return []jaeger.KeyValue{
		idTag, msgType, compressedSize, uncompressedSize,
	}
}

func ocSpanAttributesToJaegerProtoTags(ocAttribs *tracepb.Span_Attributes) []jaeger.KeyValue {
	if ocAttribs == nil {
		return make([]jaeger.KeyValue, 0)
	}

	// Pre-allocate assuming that few attributes, if any at all, are nil.
	jTags := make([]jaeger.KeyValue, 0, len(ocAttribs.AttributeMap))
	for key, attrib := range ocAttribs.AttributeMap {
		if attrib == nil || attrib.Value == nil {
			continue
		}

		jTag := jaeger.KeyValue{Key: key}
		switch attribValue := attrib.Value.(type) {
		case *tracepb.AttributeValue_StringValue:
			// Jaeger-to-OC maps binary tags to string attributes and encodes them as
			// base64 strings. Blindingly attempting to decode base64 seems too much.
			str := truncableStringToStr(attribValue.StringValue)
			jTag.VStr = str
			jTag.VType = jaeger.ValueType_STRING
		case *tracepb.AttributeValue_IntValue:
			i := attribValue.IntValue
			jTag.VInt64 = i
			jTag.VType = jaeger.ValueType_INT64
		case *tracepb.AttributeValue_BoolValue:
			b := attribValue.BoolValue
			jTag.VBool = b
			jTag.VType = jaeger.ValueType_BOOL
		case *tracepb.AttributeValue_DoubleValue:
			d := attribValue.DoubleValue
			jTag.VFloat64 = d
			jTag.VType = jaeger.ValueType_FLOAT64
		default:
			str := "<Unknown OpenCensus Attribute for key \"" + key + "\">"
			jTag.VStr = str
			jTag.VType = jaeger.ValueType_STRING
		}
		jTags = append(jTags, jTag)
	}

	return jTags
}

func ocTraceIDToJaegerProtoTraceID(traceID []byte) (jaeger.TraceID, error) {
	if traceID == nil {
		return jaeger.TraceID{}, errNilTraceID
	}
	if len(traceID) != 16 {
		return jaeger.TraceID{}, errWrongLenTraceID
	}

	return jaeger.TraceID{
		Low:  bytesToInt64(traceID[8:16]),
		High: bytesToInt64(traceID[0:8]),
	}, nil
}

func ocSpanIDToJaegerProtoSpanID(spanID []byte) (jaeger.SpanID, error) {
	var jSpanID jaeger.SpanID
	if spanID == nil {
		return jSpanID, errNilID
	}
	if len(spanID) != 8 {
		return jSpanID, errWrongLenID
	}
	return jaeger.SpanID(bytesToInt64(spanID[:])), nil
}

func bytesToInt64(buf []byte) uint64 {
	u := binary.BigEndian.Uint64(buf)
	return uint64(u)
}

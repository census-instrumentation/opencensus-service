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
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
	jaeger "github.com/jaegertracing/jaeger/model"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
)

var (
	errZeroTraceID     = errors.New("OC span has an all zeros trace ID")
	errNilTraceID      = errors.New("OC trace ID is nil")
	errWrongLenTraceID = errors.New("TraceID does not have 16 bytes")
	errZeroSpanID      = errors.New("OC span has an all zeros span ID")
	errNilID           = errors.New("OC ID is nil")
	errWrongLenID      = errors.New("ID does not have 8 bytes")
)

// OCProtoToJaegerProto translates OpenCensus trace data into the Jaeger Proto for GRPC.
func OCProtoToJaegerProto(ocBatch *agenttracepb.ExportTraceServiceRequest) (jaeger.Batch, error) {
	if ocBatch == nil {
		return nil, nil
	}

	jSpans, err := ocSpansToJaegerSpans(ocBatch.Spans)
	if err != nil {
		return nil, err
	}

	jb := &jaeger.Batch{
		Process: ocNodeToJaegerProcess(ocBatch.Node),
		Spans:   jSpans,
	}

	return jb, nil
}

// Replica of protospan_to_jaegerthrift.ocNodeToJaegerProcess
func ocNodeToJaegerProcess(node *commonpb.Node) jaeger.Process {
	if node == nil {
		return nil
	}

	var jTags []jaeger.Tag
	nodeAttribsLen := len(node.Attributes)
	if nodeAttribsLen > 0 {
		jTags = make([]jaeger.Tag, 0, nodeAttribsLen)
		for k, v := range node.Attributes {
			str := v
			jTag := &jaeger.Tag{
				Key:   k,
				VType: jaeger.TagType_STRING,
				VStr:  &str,
			}
			jTags = append(jTags, jTag)
		}
	}

	if node.Identifier != nil {
		if node.Identifier.HostName != "" {
			hostTag := jaeger.Tag{
				Key:   "hostname",
				VType: jaeger.TagType_STRING,
				VStr:  &node.Identifier.HostName,
			}
			jTags = append(jTags, hostTag)
		}
		if node.Identifier.Pid != 0 {
			pid := int64(node.Identifier.Pid)
			hostTag := jaeger.Tag{
				Key:   "pid",
				VType: jaeger.TagType_LONG,
				VLong: &pid,
			}
			jTags = append(jTags, hostTag)
		}
		if node.Identifier.StartTimestamp != nil && node.Identifier.StartTimestamp.Seconds != 0 {
			startTimeStr := ptypes.TimestampString(node.Identifier.StartTimestamp)
			hostTag := jaeger.Tag{
				Key:   "start.time",
				VType: jaeger.TagType_STRING,
				VStr:  &startTimeStr,
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
			languageTag := jaeger.Tag{
				Key:   "opencensus.language",
				VType: jaeger.TagType_STRING,
				VStr:  &languageStr,
			}
			jTags = append(jTags, languageTag)
		}
		if ocLib.ExporterVersion != "" {
			exporterTag := jaeger.Tag{
				Key:   "opencensus.exporterversion",
				VType: jaeger.TagType_STRING,
				VStr:  &ocLib.ExporterVersion,
			}
			jTags = append(jTags, exporterTag)
		}
		if ocLib.CoreLibraryVersion != "" {
			exporterTag := jaeger.Tag{
				Key:   "opencensus.corelibversion",
				VType: jaeger.TagType_STRING,
				VStr:  &ocLib.CoreLibraryVersion,
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
		return nil
	}

	jProc := &jaeger.Process{
		ServiceName: serviceName,
		Tags:        jTags,
	}

	return jProc
}

func truncableStringToStr(ts *tracepb.TruncatableString) string {
	if ts == nil {
		return ""
	}
	return ts.Value
}

func ocLinksToJaegerReferences(ocSpanLinks *tracepb.Span_Links) ([]jaeger.SpanRef, error) {
	if ocSpanLinks == nil || ocSpanLinks.Link == nil {
		return nil, nil
	}

	ocLinks := ocSpanLinks.Link
	jRefs := make([]jaeger.SpanRef, 0, len(ocLinks))
	for _, ocLink := range ocLinks {
		var traceId [16]byte
		if ocLink.TraceId != nil {
			traceId = ocLink.TraceId
		} else {
			return nil, fmt.Errorf("OC link has invalid trace ID: %v", err)
		}

		var spanId [8]byte
		if ocLink.SpanId != nil {
			spanId = ocLink.SpanId
		} else {
			return nil, fmt.Errorf("OC link has invalid span ID: %v", err)
		}

		var jRefType jaeger.SpanRefType
		switch ocLink.Type {
		case tracepb.Span_Link_PARENT_LINKED_SPAN:
			jRefType = jaeger.SpanRefType_CHILD_OF
		default:
			// TODO: Jaeger doesn't have a unknown SpanRefType, it has FOLLOWS_FROM or CHILD_OF
			// at first mapping all others to FOLLOWS_FROM.
			jRefType = jaeger.SpanRefType_FOLLOWS_FROM
		}

		jRef := &jaeger.SpanRef{
			TraceId: traceId,
			SpanId:  spanId,
			RefType: jRefType,
		}
		jRefs = append(jRefs, jRef)
	}

	return jRefs, nil
}

func timestampToTime(ts *timestamp.Timestamp) (t time.Time) {
	if ts == nil {
		return
	}
	return time.Unix(ts.Seconds, int64(ts.Nanos))
}

// Replica of protospan_to_jaegerthrift.ocSpanAttributesToJaegerTags
func ocSpanAttributesToJaegerTags(ocAttribs *tracepb.Span_Attributes) []jaeger.KeyValue {
	if ocAttribs == nil {
		return nil
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
			jTag.VStr = &str
			jTag.VType = jaeger.ValueType_STRING
		case *tracepb.AttributeValue_IntValue:
			i := attribValue.IntValue
			jTag.VInt64 = &i
			jTag.VType = jaeger.ValueType_INT64
		case *tracepb.AttributeValue_BoolValue:
			b := attribValue.BoolValue
			jTag.VBool = &b
			jTag.VType = jaeger.ValueType_BOOL
		case *tracepb.AttributeValue_DoubleValue:
			d := attribValue.DoubleValue
			jTag.VInt64 = &d
			jTag.VType = jaeger.ValueType_INT64
		default:
			str := "<Unknown OpenCensus Attribute for key \"" + key + "\">"
			jTag.VStr = &str
			jTag.VType = jaeger.ValueType_STRING
		}
		jTags = append(jTags, jTag)
	}

	return jTags
}

func ocTimeEventsToJaegerLogs(ocSpanTimeEvents *tracepb.Span_TimeEvents) []jaeger.Log {
	if ocSpanTimeEvents == nil || ocSpanTimeEvents.TimeEvent == nil {
		return nil
	}

	ocTimeEvents := ocSpanTimeEvents.TimeEvent

	// Assume that in general no time events are going to produce nil Jaeger logs.
	jLogs := make([]jaeger.Log, 0, len(ocTimeEvents))
	for _, ocTimeEvent := range ocTimeEvents {
		jLog := jaeger.Log{
			Timestamp: timestampToTime(ocTimeEvent.Time),
		}
		switch teValue := ocTimeEvent.Value.(type) {
		case *tracepb.Span_TimeEvent_Annotation_:
			jLog.Fields = ocAnnotationToJagerTags(teValue.Annotation)
		case *tracepb.Span_TimeEvent_MessageEvent_:
			jLog.Fields = ocMessageEventToJaegerTags(teValue.MessageEvent)
		default:
			msg := "An unknown OpenCensus TimeEvent type was detected when translating to Jaeger"
			jKV := jaeger.KeyValue{
				Key:  "unknown.oc.timeevent.type",
				VStr: &msg,
			}
			jLog.Fields = append(jLog.Fields, jKV)
		}

		jLogs = append(jLogs, jLog)
	}

	return jLogs
}

func ocAnnotationToJagerTags(annotation *tracepb.Span_TimeEvent_Annotation) []jaeger.KeyValue {
	if annotation == nil {
		return nil
	}

	// TODO: Find a better tag for Annotation description.
	jKV := jaeger.KeyValue{
		Key:  "oc.timeevent.annotation.description",
		VStr: annotation.Description,
	}
	return append(ocSpanAttributesToJaegerTags(annotation.Attributes), jKV)
}

func ocMessageEventToJaegerTags(msgEvent *tracepb.Span_TimeEvent_MessageEvent) []jaeger.KeyValue {
	if msgEvent == nil {
		return nil
	}

	msgEventId := make([]byte, 8)
	binary.BigEndian.PutUint64(msgEventId, uint64(msgEvent.Id))

	uncompressedSize := make([]byte, 8)
	binary.BigEndian.PutUint64(uncompressedSize, uint64(msgEvent.UncompressedSize))

	compressedSize := make([]byte, 8)
	binary.BigEndian.PutUint64(compressedSize, uint64(msgEvent.CompressedSize))

	// TODO: Find a better tag for Message event.
	jaegerKVs := []jaeger.KeyValue{
		jaeger.KeyValue{
			Key:    "oc.timeevent.messageevent.type",
			VInt64: msgEvent.Type,
		},
		jaeger.KeyValue{
			Key:     "oc.timeevent.messageevent.id",
			VBinary: msgEventId,
		},
		jaeger.KeyValue{
			Key:     "oc.timeevent.messageevent.usize",
			VBinary: uncompressedSize,
		},
		jaeger.KeyValue{
			Key:     "oc.timeevent.messageevent.csize",
			VBinary: compressedSize,
		},
	}
	return jaegerKVs
}

// Replica of protospan_to_jaegerthrift appendJaegerTagFromOCSpanKind
func appendJaegerTagFromOCSpanKind(jTags []jaeger.Tag, ocSpanKind tracepb.Span_SpanKind) []jaeger.Tag {
	// We could check if the key is already present but it doesn't seem worth at this point.
	// TODO: (@pjanotti): Replace any OpenTracing literals by importing github.com/opentracing/opentracing-go/ext?
	var tagValue string
	switch ocSpanKind {
	case tracepb.Span_CLIENT:
		tagValue = "client"
	case tracepb.Span_SERVER:
		tagValue = "server"
	}

	if tagValue != "" {
		jTag := jaeger.Tag{
			Key:  "span.kind",
			VStr: &tagValue,
		}
		jTags = append(jTags, jTag)
	}

	return jTags
}

func appendJaegerTagFromOCTracestate(jTags []jaeger.Tag, ocSpanTracestate *tracepb.Tracestate) []jaeger.Tag {
	if ocSpanTracestate == nil {
		return jTags
	}

	for _, tsEntry := range ocSpanTracestate.Entries {
		jTag := jaeger.Tag{
			Key:  tsEntry.Key,
			VStr: ts.Entry.Value,
		}
		jTags = append(jTags, jTag)
	}

	return jTags
}

func appendJaegerTagFromOCStatus(jTags []jaeger.Tag, ocStatus *tracepb.Status) []jaeger.Tag {
	if ocStatus == nil {
		return nil
	}

	jTag := jaeger.Tag{
		Key:    "span.status",
		VInt64: ocStatus.code,
		VStr:   ocStatus.Message,
	}
	jTags = append(jTags, jTag)

	return jTags
}

func appendJaegerTagFromOCSameProcessAsParentSpan(jTags []jaeger.Tag, ocSameProcessAsParentSpan *tracepb.BoolValue) []jaeger.Tag {
	if ocSameProcessAsParentSpan == nil {
		return nil
	}

	jTag := jaeger.Tag{
		Key:   "span.sameprocessasparentspan",
		VBool: ocSameProcessAsParentSpan.Value,
	}
	jTags = append(jTags, jTag)

	return jTags
}

func appendJaegerTagFromOCChildSpanCount(jTags []jaeger.Tag, ocChildSpanCount *tracepb.UInt32Value) []jaeger.Tag {
	if ocChildSpanCount == nil {
		return nil
	}

	jTag := jaeger.Tag{
		Key:    "span.childcount",
		VInt64: ocChildSpanCount.Value,
	}
	jTags = append(jTags, jTag)

	return jTags
}

func ocSpansToJaegerSpans(ocSpans []*tracepb.Span) ([]*jaeger.Span, error) {
	if ocSpans == nil {
		return nil, nil
	}

	// Pre-allocate assuming that few, if any spans, are nil.
	jSpans := make([]*jaeger.Span, 0, len(ocSpans))
	for _, ocSpan := range ocSpans {
		var traceId [16]byte
		if ocSpan.TraceId == nil {
			return nil, errNilTraceID
		} else if len(ocSpan.TraceId) != 16 {
			return nil, errWrongLenTraceID
		} else if ocSpan.TraceId == 0 {
			return nil, errZeroTraceID
		} else {
			traceId = ocSpan.TraceId
		}

		var spanId [8]byte
		if ocSpan.SpanId == nil {
			return nil, errNilID
		} else if len(ocSpan.SpanId) != 8 {
			return nil, errWrongLenID
		} else if ocSpan.SpanId == 0 {
			return nil, errZeroSpanID
		} else {
			spanId = ocSpan.SpanId
		}

		jReferences, err := ocLinksToJaegerReferences(ocSpan.Links)
		if err != nil {
			return nil, fmt.Errorf("Error converting OC links to Jaeger references: %v", err)
		}

		// OC ParentSpanId can be nil/empty: only attempt conversion if not nil/empty.
		if len(ocSpan.ParentSpanId) != 0 {
			jRef := &jaeger.SpanRef{
				TraceId: traceId,
				SpanId:  ocSpan.ParentSpanId,
				RefType: jaeger.SpanRefType_CHILD_OF,
			}
		}

		startTime := timestampToTime(ocSpan.StartTime)
		jSpan := &jaeger.Span{
			TraceID:       traceId,
			SpanId:        spanId,
			OperationName: truncableStringToStr(ocSpan.Name),
			References:    jReferences,
			StartTime:     startTime,
			Duration:      timestampToTime(ocSpan.EndTime).Sub(startTime),
			Tags:          ocSpanAttributesToJaegerTags(ocSpan.Attributes),
			Logs:          ocTimeEventsToJaegerLogs(ocSpan.TimeEvents),
			// Flags: TODO Flags might be used once sampling and other features are implemented in OC.
		}

		jSpan.Tags = appendJaegerTagFromOCTracestate(jSpan.Tags, ocSpan.Tracestate)
		jSpan.Tags = appendJaegerTagFromOCSpanKind(jSpan.Tags, ocSpan.Kind)
		jSpan.Tags = appendJaegerTagFromOCStatus(jSpan.Tags, ocSpan.Status)
		jSpan.Tags = appendJaegerTagFromOCSameProcessAsParentSpan(jSpan.Tags, ocSpan.SameProcessAsParentSpan)
		jSpan.Tags = appendJaegerTagFromOCChildSpanCount(jSpan.Tags, ocSpan.ChildSpanCount)
		jSpans = append(jSpans, jSpan)
	}

	return jSpans, nil
}

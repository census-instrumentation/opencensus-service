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

	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/jaegertracing/jaeger/thrift-gen/jaeger"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
)

// OCProtoToJaegerThrift translates OpenCensus trace data into the Jaeger Thrift format.
func OCProtoToJaegerThrift(ocBatch *agenttracepb.ExportTraceServiceRequest) (*jaeger.Batch, error) {
	jb := &jaeger.Batch{
		Process: ocNodeToJaegerProcess(ocBatch.Node),
		Spans:   ocSpansToJaegerSpans(ocBatch.Spans),
	}
	return jb, nil
}

func ocNodeToJaegerProcess(node *commonpb.Node) *jaeger.Process {
	if node == nil {
		return nil
	}

	var jTags []*jaeger.Tag
	nodeAttribsLen := len(node.Attributes)
	if nodeAttribsLen > 0 {
		jTags = make([]*jaeger.Tag, 0, nodeAttribsLen)
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

	if node.Identifier != nil && node.Identifier.HostName != "" {
		hostTag := &jaeger.Tag{
			Key:   "hostname",
			VType: jaeger.TagType_STRING,
			VStr:  &node.Identifier.HostName,
		}
		jTags = append(jTags, hostTag)
	}

	// Add OpenCensus library information as tags if available
	ocLib := node.LibraryInfo
	if ocLib != nil {
		// Only add language if specified
		if ocLib.Language != commonpb.LibraryInfo_LANGUAGE_UNSPECIFIED {
			languageStr := ocLib.Language.String()
			languageTag := &jaeger.Tag{
				Key:   "opencensus.language",
				VType: jaeger.TagType_STRING,
				VStr:  &languageStr,
			}
			jTags = append(jTags, languageTag)
		}
		if ocLib.ExporterVersion != "" {
			exporterTag := &jaeger.Tag{
				Key:   "opencensus.exporterversion",
				VType: jaeger.TagType_STRING,
				VStr:  &ocLib.ExporterVersion,
			}
			jTags = append(jTags, exporterTag)
		}
		if ocLib.CoreLibraryVersion != "" {
			exporterTag := &jaeger.Tag{
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

func ocSpansToJaegerSpans(ocSpans []*tracepb.Span) []*jaeger.Span {
	if ocSpans == nil {
		return nil
	}

	// Pre-allocate assuming that few, if any spans, are nil.
	jSpans := make([]*jaeger.Span, 0, len(ocSpans))
	for _, ocSpan := range ocSpans {
		startTime := timestampToEpochMicroseconds(ocSpan.StartTime)
		jSpan := &jaeger.Span{
			SpanId:        ocIDBytesToJaegerID(ocSpan.SpanId),
			ParentSpanId:  ocIDBytesToJaegerID(ocSpan.ParentSpanId),
			OperationName: truncableStringToStr(ocSpan.Name),
			References:    ocLinksToJaegerReferences(ocSpan.Links),
			// Flags: TODO (@pjanotti) at first nothing matches to it.
			StartTime: startTime,
			Duration:  timestampToEpochMicroseconds(ocSpan.EndTime) - startTime,
			Tags:      ocSpanAttributesToJaegerTags(ocSpan.Attributes),
			Logs:      ocTimeEventsToJaegerLogs(ocSpan.TimeEvents),
		}

		jSpan.Tags = appendJaegerTagFromOCSpanKind(jSpan.Tags, ocSpan.Kind)
		jSpan.TraceIdLow, jSpan.TraceIdHigh = traceIDBytesToLowAndHigh(ocSpan.TraceId)
		jSpans = append(jSpans, jSpan)
	}

	return jSpans
}

func ocLinksToJaegerReferences(ocSpanLinks *tracepb.Span_Links) []*jaeger.SpanRef {
	if ocSpanLinks == nil || ocSpanLinks.Link == nil {
		return nil
	}

	ocLinks := ocSpanLinks.Link
	jRefs := make([]*jaeger.SpanRef, 0, len(ocLinks))
	for _, ocLink := range ocLinks {
		var jRefType jaeger.SpanRefType
		switch ocLink.Type {
		case tracepb.Span_Link_PARENT_LINKED_SPAN:
			jRefType = jaeger.SpanRefType_CHILD_OF
		default:
			// TODO: (@pjanotti) Jaeger doesn't have a unknown SpanRefType, it has FOLLOWS_FROM or CHILD_OF
			// at first mapping all others to FOLLOWS_FROM.
			jRefType = jaeger.SpanRefType_FOLLOWS_FROM
		}

		jRef := &jaeger.SpanRef{
			RefType: jRefType,
			SpanId:  ocIDBytesToJaegerID(ocLink.SpanId),
		}
		jRef.TraceIdLow, jRef.TraceIdHigh = traceIDBytesToLowAndHigh(ocLink.TraceId)
		jRefs = append(jRefs, jRef)
	}

	return jRefs
}

func appendJaegerTagFromOCSpanKind(jTags []*jaeger.Tag, ocSpanKind tracepb.Span_SpanKind) []*jaeger.Tag {
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
		jTag := &jaeger.Tag{
			Key:  "span.kind",
			VStr: &tagValue,
		}
		jTags = append(jTags, jTag)
	}

	return jTags
}

func ocTimeEventsToJaegerLogs(ocSpanTimeEvents *tracepb.Span_TimeEvents) []*jaeger.Log {
	if ocSpanTimeEvents == nil || ocSpanTimeEvents.TimeEvent == nil {
		return nil
	}

	ocTimeEvents := ocSpanTimeEvents.TimeEvent

	// Assume that in general no time events are going to produce nil Jaeger logs.
	jLogs := make([]*jaeger.Log, 0, len(ocTimeEvents))
	for _, ocTimeEvent := range ocTimeEvents {
		jLog := &jaeger.Log{
			Timestamp: timestampToEpochMicroseconds(ocTimeEvent.Time),
		}
		switch teValue := ocTimeEvent.Value.(type) {
		case *tracepb.Span_TimeEvent_Annotation_:
			jLog.Fields = ocAnnotationToJagerTags(teValue.Annotation)
		case *tracepb.Span_TimeEvent_MessageEvent_:
			jLog.Fields = ocMessageEventToJaegerTags(teValue.MessageEvent)
		default:
			msg := "An unknown OpenCensus TimeEvent type was detected when translating to Jaeger"
			jTag := &jaeger.Tag{
				Key:  "unknown.oc.timeevent.type",
				VStr: &msg,
			}
			jLog.Fields = append(jLog.Fields, jTag)
		}

		jLogs = append(jLogs, jLog)
	}

	return jLogs
}

func ocAnnotationToJagerTags(annotation *tracepb.Span_TimeEvent_Annotation) []*jaeger.Tag {
	if annotation == nil {
		return nil
	}

	// TODO: (@pjanotti) what about Description? Does it fit as another tag?

	return ocSpanAttributesToJaegerTags(annotation.Attributes)
}

func ocMessageEventToJaegerTags(msgEvent *tracepb.Span_TimeEvent_MessageEvent) []*jaeger.Tag {
	if msgEvent == nil {
		return nil
	}

	// TODO: (@pjanotti) Not clear how to map those to Jaeger, perhaps some OpenTracing tags...

	return nil
}

func truncableStringToStr(ts *tracepb.TruncatableString) string {
	if ts == nil {
		return ""
	}
	return ts.Value
}

func traceIDBytesToLowAndHigh(traceID []byte) (traceIDLow, traceIDHigh int64) {
	if len(traceID) != 16 {
		return
	}
	traceIDHigh = int64(binary.BigEndian.Uint64(traceID[:8]))
	traceIDLow = int64(binary.BigEndian.Uint64(traceID[8:]))
	return
}

func ocIDBytesToJaegerID(b []byte) (id int64) {
	if len(b) != 8 {
		return
	}

	id = int64(binary.BigEndian.Uint64(b))
	return
}

func timestampToEpochMicroseconds(ts *timestamp.Timestamp) int64 {
	if ts == nil {
		return 0
	}
	return ts.GetSeconds()*1e6 + int64(ts.GetNanos()/1e3)
}

func ocSpanAttributesToJaegerTags(ocAttribs *tracepb.Span_Attributes) []*jaeger.Tag {
	if ocAttribs == nil {
		return nil
	}

	// Pre-allocate assuming that few attributes, if any at all, are nil.
	jTags := make([]*jaeger.Tag, 0, len(ocAttribs.AttributeMap))
	for key, attrib := range ocAttribs.AttributeMap {
		if attrib == nil || attrib.Value == nil {
			continue
		}

		jTag := &jaeger.Tag{Key: key}
		switch attribValue := attrib.Value.(type) {
		case *tracepb.AttributeValue_StringValue:
			// Jaeger-to-OC maps binary tags to string attributes and encodes them as
			// base64 strings. Blindingly attempting to decode base64 seems too much.
			str := truncableStringToStr(attribValue.StringValue)
			jTag.VStr = &str
			jTag.VType = jaeger.TagType_STRING
		case *tracepb.AttributeValue_IntValue:
			i := attribValue.IntValue
			jTag.VLong = &i
			jTag.VType = jaeger.TagType_LONG
		case *tracepb.AttributeValue_BoolValue:
			b := attribValue.BoolValue
			jTag.VBool = &b
			jTag.VType = jaeger.TagType_BOOL
		case *tracepb.AttributeValue_DoubleValue:
			d := attribValue.DoubleValue
			jTag.VDouble = &d
			jTag.VType = jaeger.TagType_DOUBLE
		default:
			str := "<Unknown OpenCensus Attribute for key \"" + key + "\">"
			jTag.VStr = &str
			jTag.VType = jaeger.TagType_STRING
		}
		jTags = append(jTags, jTag)
	}

	return jTags
}

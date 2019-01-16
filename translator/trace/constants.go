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

const (
	// Jaeger Tags
	JaegerTagForOcUnknownTimeEventType           = "unknown.oc.timeevent.type"
	JaegerTagForOcTimeEventAnnotationDescription = "oc.timeevent.annotation.description"
	JaegerTagForOcTimeEventMessageEventType      = "oc.timeevent.messageevent.type"
	JaegerTagForOcTimeEventMessageEventId        = "oc.timeevent.messageevent.id"
	JaegerTagForOcTimeEventMessageEventUSize     = "oc.timeevent.messageevent.usize"
	JaegerTagForOcTimeEventMessageEventCSize     = "oc.timeevent.messageevent.csize"
	JaegerTagForOcSameProcessAsParentSpan        = "oc.sameprocessasparentspan"
	JaegerTagForOcChildSpanCount                 = "oc.span.childcount"
)

var (
	ErrZeroTraceID     = errors.New("OC span has an all zeros trace ID")
	ErrNilTraceID      = errors.New("OC trace ID is nil")
	ErrWrongLenTraceID = errors.New("TraceID does not have 16 bytes")
	ErrZeroSpanID      = errors.New("OC span has an all zeros span ID")
	ErrNilID           = errors.New("OC ID is nil")
	ErrWrongLenID      = errors.New("ID does not have 8 bytes")
)

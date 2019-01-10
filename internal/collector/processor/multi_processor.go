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

package processor

import (
	"fmt"
	"strings"

	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
)

type option func(*multiSpanProcessor)
type preProcessFn func(*agenttracepb.ExportTraceServiceRequest, string)

// MultiSpanProcessor enables processing on multiple processors.
// For each incoming span batch, it calls ProcessSpans method on each span
// processor one-by-one. It aggregates success/failures/errors from all of
// them and reports the result upstream.
type multiSpanProcessor struct {
	processors    []SpanProcessor
	preProcessFns []preProcessFn
}

// NewMultiSpanProcessor creates a multiSpanProcessor from the variadic
// list of passed SpanProcessors and options.
func NewMultiSpanProcessor(procs []SpanProcessor, options ...option) SpanProcessor {
	multiSpanProcessor := &MultiSpanProcessor{
		processors: procs,
	}
	for _, opt := range options {
		opt(multiSpan)
	}
	return multiSpanProcessor
}

func WithPreProcessFn(preProcFn preProcessFn) option {
	return func(msp *multiSpanProcessor) {
		msp.preProcessFns = append(msp.preProcessFns, preProcessFn)
	}
}

func WithAddAttributes(attributes map[string]*AttributeValue, overwrite bool) option {
	return WithPreProcessFn(
		func(batch *agenttracepb.ExportTraceServiceRequest, spanFormat string) {
			if len(attributes) == 0 {
				return
			}
			for _, span := range batch.Spans {
				if span.Attributes == nil {
					span.Attributes = &tracepb.Span_Attributes{}
				}
				// Create a new map if one does not exist. Could re-use passed in map, but
				// feels too unsafe.
				if span.Attributes.AttributeMap == nil {
					span.Attributes.AttributeMap = make(map[string]*AttributeValue, len(attributes))
				}
				// Copy all attributes that need to be added into the span's attribute map.
				// If a key already exists, we will only overwrite is the overwrite flag
				// is set to true.
				for key, value := range attributes {
					if _, exists := span.Attributes.AttributeMap[key]; overwrite || !exists {
						span.Attributes.AttributeMap[key] = value
					}
				}
			}
		},
	)
}

// ProcessSpans implements the SpanProcessor interface
func (msp *multiSpanProcessor) ProcessSpans(batch *agenttracepb.ExportTraceServiceRequest, spanFormat string) (uint64, error) {
	for _, preProcessFn := range msp.preProcessFns {
		preProcessFn(batch, spanFormat)
	}
	var maxFailures uint64
	var errors []error
	for _, sp := range msp.processors {
		failures, err := sp.ProcessSpans(batch, spanFormat)
		if err != nil {
			errors = append(errors, err)
		}

		if failures > maxFailures {
			maxFailures = failures
		}
	}

	var err error
	numErrors := len(errors)
	if numErrors == 1 {
		err = errors[0]
	} else if numErrors > 1 {
		errMsgs := make([]string, numErrors)
		for _, err := range errors {
			errMsgs = append(errMsgs, err.Error())
		}
		err = fmt.Errorf("[%s]", strings.Join(errMsgs, "; "))
	}

	return maxFailures, err
}

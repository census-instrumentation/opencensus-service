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
	jaeger "github.com/jaegertracing/jaeger/model"

	converter "github.com/jaegertracing/jaeger/model/converter/thrift/jaeger"

	// commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	// tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
)

// OCProtoToJaegerProto translates OpenCensus trace data into the Jaeger Protobuf format using Jaeger thrift as intermediary.
func OCProtoToJaegerProto(ocBatch *agenttracepb.ExportTraceServiceRequest) (*jaeger.Batch, error) {
	if ocBatch == nil {
		return nil, nil
	}

	tBatch, err := OCProtoToJaegerThrift(ocBatch)
	if err != nil {
		return nil, err
	}

	jSpans := converter.ToDomain(tBatch.Spans, tBatch.Process)
	var jProcess jaeger.Process
	if len(jSpans) > 0 {
		jProcess = *jSpans[0].Process
	}

	jb := &jaeger.Batch{
		Process: jProcess,
		Spans:   jSpans,
	}

	return jb, nil
}

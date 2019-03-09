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
	"context"

	"github.com/census-instrumentation/opencensus-service/consumer"
	"github.com/census-instrumentation/opencensus-service/data"
)

type protoProcessorSink struct {
	protoProcessor SpanProcessor
}

var _ (consumer.TraceConsumer) = (*protoProcessorSink)(nil)

// WrapWithSpanSink wraps a processor to be used as a span sink by receivers.
func WrapWithSpanSink(p SpanProcessor) consumer.TraceConsumer {
	return &protoProcessorSink{
		protoProcessor: p,
	}
}

func (ps *protoProcessorSink) ConsumeTraceData(ctx context.Context, td data.TraceData) error {
	return ps.protoProcessor.ProcessSpans(ctx, td)
}

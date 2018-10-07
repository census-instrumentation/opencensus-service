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

package spanreceiver

import (
	"context"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
)

// SpanReceiver is an interface that receives spans from a Node identifier.
type SpanReceiver interface {
	ReceiveSpans(ctx context.Context, node *commonpb.Node, spans ...*tracepb.Span) (*Acknowledgement, error)
}

type Acknowledgement struct {
	SavedSpans   uint64
	DroppedSpans uint64
}

type multiSpanReceiver []SpanReceiver

// Multi is a helper function that is useful to combine multiple
// SpanReceiver instances. It serves a purpose similar to io.MultiReader.
func Multi(spanreceivers ...SpanReceiver) SpanReceiver {
	return multiSpanReceiver(spanreceivers)
}

func (msr multiSpanReceiver) ReceiveSpans(ctx context.Context, node *commonpb.Node, spans ...*tracepb.Span) (*Acknowledgement, error) {
	for _, sr := range msr {
		_, _ = sr.ReceiveSpans(ctx, node, spans...)
	}
	return &Acknowledgement{SavedSpans: uint64(len(spans))}, nil
}

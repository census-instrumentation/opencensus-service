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

package sender

import (
	"go.uber.org/zap"

	reporter "github.com/jaegertracing/jaeger/cmd/agent/app/reporter"

	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	"github.com/census-instrumentation/opencensus-service/internal/collector/processor"
	"github.com/census-instrumentation/opencensus-service/translator/trace"
)

// JaegerThriftTChannelSender takes span batches and sends them
// out on tchannel in thrift encoding
type JaegerThriftTChannelSender struct {
	logger   *zap.Logger
	reporter reporter.Reporter
}

var _ processor.SpanProcessor = (*JaegerThriftHTTPSender)(nil)

// NewJaegerThriftTChannelSender creates new TChannel-based sender.
func NewJaegerThriftTChannelSender(
	reporter reporter.Reporter,
	zlogger *zap.Logger,
) *JaegerThriftTChannelSender {
	return &JaegerThriftTChannelSender{
		logger:   zlogger,
		reporter: reporter,
	}
}

// ProcessSpans sends the received data to the configured Jaeger Thrift end-point.
func (s *JaegerThriftTChannelSender) ProcessSpans(batch *agenttracepb.ExportTraceServiceRequest, spanFormat string) (uint64, error) {
	// TODO: (@pjanotti) In case of failure the translation to Jaeger Thrift is going to be remade, cache it somehow.
	tBatch, err := tracetranslator.OCProtoToJaegerThrift(batch)
	if err != nil {
		return uint64(len(tBatch.Spans)), err
	}

	if err := s.reporter.EmitBatch(tBatch); err != nil {
		s.logger.Error("Reporter failed to report span batch", zap.Error(err))
		return uint64(len(tBatch.Spans)), err
	}
	return 0, nil
}

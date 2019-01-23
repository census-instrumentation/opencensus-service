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
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	"github.com/omnition/opencensus-go-exporter-kinesis"
	"go.uber.org/zap"

	"github.com/census-instrumentation/opencensus-service/cmd/occollector/app/builder"
	"github.com/census-instrumentation/opencensus-service/internal/collector/processor"
	"github.com/census-instrumentation/opencensus-service/translator/trace"
)

// KinesisSender takes span batches and sends them
// out on an AWS Kinesis stream in the specified format
type KinesisSender struct {
	logger   *zap.Logger
	exporter *kinesis.Exporter
}

var _ processor.SpanProcessor = (*KinesisSender)(nil)

// NewKinesisSender creates new jaeger proto based Kinesis sender.
func NewKinesisSender(
	opts *builder.KinesisSenderCfg,
	zlogger *zap.Logger,
) (*KinesisSender, error) {
	exporter, err := kinesis.NewExporter(kinesis.Options{
		StreamName:              opts.StreamName,
		AWSRegion:               opts.AWSRegion,
		AWSRole:                 opts.AWSRole,
		AWSKinesisEndpoint:      opts.AWSKinesisEndpoint,
		KPLBatchSize:            opts.KPLBatchSize,
		KPLBatchCount:           opts.KPLBatchCount,
		KPLBacklogCount:         opts.KPLBacklogCount,
		KPLFlushIntervalSeconds: opts.KPLFlushIntervalSeconds,
		Encoding:                opts.Encoding,
	}, zlogger)
	if err != nil {
		return nil, err
	}
	return &KinesisSender{
		logger:   zlogger,
		exporter: exporter,
	}, nil
}

// ProcessSpans sends the received data to the configured Jaeger Proto end-point.
func (s *KinesisSender) ProcessSpans(batch *agenttracepb.ExportTraceServiceRequest, spanFormat string) (uint64, error) {
	pBatch, err := tracetranslator.OCProtoToJaegerProto(batch)
	if err != nil {
		s.logger.Error("error translating span batch", zap.Error(err))
		return uint64(len(pBatch.Spans)), err
	}
	failed := uint64(0)
	var exportErr error
	for _, span := range pBatch.GetSpans() {
		err := s.exporter.ExportSpan(span)
		if err != nil {
			s.logger.Error("error exporting span to kinesis", zap.Error(err))
			exportErr = err
			failed += 1
		}
	}
	return failed, exportErr
}

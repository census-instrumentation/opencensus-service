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

package sender

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	jaegerproto "github.com/jaegertracing/jaeger/proto-gen/api_v2"

	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	tracetranslator "github.com/census-instrumentation/opencensus-service/translator/trace"
)

// JaegerProtoGRPCSender forwards spans encoded in the jaeger proto
// format to a grpc server
type JaegerProtoGRPCSender struct {
	client *grpc.ClientConn
	logger *zap.Logger
}

// NewJaegerProtoGRPCSender returns a new GRPC-backend span sender.
// The collector endpoint should be of the form "hostname:14250"
func NewJaegerProtoGRPCSender(collectorEndpoint string, zlogger *zap.Logger) *JaegerProtoGRPCSender {
	client := grpc.Dial(collectorEndpoint, grpc.WithInsecure())
	s := &JaegerProtoGRPCSender{
		client: client,
		logger: zlogger,
	}

	return s
}

// ProcessSpans sends the received data to the configured Jaeger Proto-GRPC end-point.
func (s *JaegerProtoGRPCSender) ProcessSpans(batch *agenttracepb.ExportTraceServiceRequest, spanFormat string) (uint64, error) {
	if batch == nil {
		return 0, fmt.Errorf("Jaeger sender received nil batch")
	}

	protoBatch, err := tracetranslator.OCProtoToJaegerProto(batch)
	if err != nil {
		return uint64(len(batch.Spans)), err
	}

	collectorServiceClient := jaegerproto.NewCollectorServiceClient(s.client)
	_, err := collectorServiceClient.PostSpans(context.Background(), &jaegerproto.PostSpansRequest{Batch: protoBatch})
	if err != nil {
		return 0, err
	}
	
	return protoBatch.len(), nil
}
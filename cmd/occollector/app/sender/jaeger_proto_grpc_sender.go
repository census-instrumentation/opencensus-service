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

	"go.uber.org/zap"
	"google.golang.org/grpc"

	jaegerproto "github.com/jaegertracing/jaeger/proto-gen/api_v2"

	"github.com/census-instrumentation/opencensus-service/data"
	jaegertranslator "github.com/census-instrumentation/opencensus-service/translator/trace/jaeger"
)

// JaegerProtoGRPCSender forwards spans encoded in the jaeger proto
// format, to a grpc server.
type JaegerProtoGRPCSender struct {
	client jaegerproto.CollectorServiceClient
	logger *zap.Logger
}

// NewJaegerProtoGRPCSender returns a new GRPC-backend span sender.
// The collector endpoint should be of the form "hostname:14250".
func NewJaegerProtoGRPCSender(collectorEndpoint string, zlogger *zap.Logger) *JaegerProtoGRPCSender {
	client, _ := grpc.Dial(collectorEndpoint, grpc.WithInsecure())
	collectorServiceClient := jaegerproto.NewCollectorServiceClient(client)
	s := &JaegerProtoGRPCSender{
		client: collectorServiceClient,
		logger: zlogger,
	}

	return s
}

// ProcessSpans sends the batch to the configured Jaeger Proto-GRPC endpoint.
func (s *JaegerProtoGRPCSender) ProcessSpans(td data.TraceData, spanFormat string) error {
	protoBatch, err := jaegertranslator.OCProtoToJaegerProto(td)
	if err != nil {
		return err
	}

	_, err = s.client.PostSpans(context.Background(), &jaegerproto.PostSpansRequest{Batch: *protoBatch})
	if err != nil {
		return err
	}

	return nil
}

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
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	"github.com/census-instrumentation/opencensus-service/translator/trace"
)

// JaegerProtoGRPCSender forwards spans encoded in the jaeger proto
// format to a grpc server
type JaegerProtoGRPCSender struct {
	ctx    context.Context
	client *grpc.ClientConn
	logger *zap.Logger
}

// NewJaegerProtoGRPCSender returns a new GRPC-backend span sender.
func NewJaegerProtoGRPCSender(
	headers map[string]string,
	zlogger *zap.Logger,
) *JaegerProtoGRPCSender {
	s := &JaegerProtoGRPCSender{
		headers: headers,
		client:  &grpc.ClientConn{},
		logger:  zlogger,
	}

	return s
}

// ProcessSpans sends the received data to the configured Jaeger Thrift end-point.
func (s *JaegerProtoGRPCSender) ProcessSpans(batch *agenttracepb.ExportTraceServiceRequest, spanFormat string) (uint64, error) {
	if batch == nil {
		return 0, fmt.Errorf("Jaeger sender received nil batch")
	}

	protoBatch, err := tracetranslator.OCProtoToJaegerProto(batch)
	if err != nil {
		return uint64(len(batch.Spans)), err
	}

	mSpans := pBatch.Spans
	body, err := serializeProto(pBatch)
	if err != nil {
		return uint64(len(mSpans)), err
	}

	// FIXME
	req, err := grpc.ClientConn.Invoke("POST", s.url, body)
	if err != nil {
		return uint64(len(mSpans)), err
	}
	req.Header.Set("Content-Type", "application/x-thrift")
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}
	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return uint64(len(mSpans)), fmt.Errorf("Jaeger Thirft HTTP sender error: %d", resp.StatusCode)
	}
	return 0, nil
}

func serializeProto() (*bytes.Buffer, error) {
	t := thrift.NewTMemoryBuffer()
	p := thrift.NewTBinaryProtocolTransport(t)
	if err := obj.Write(p); err != nil {
		return nil, err
	}
	return t.Buffer, nil
}

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

// Package jaegerreceiver wraps the functionality to start the end-point that
// receives data directly in the Jaeger format as jaeger-collector (UDP as
// jaeger-agent currently is not supported).
package jaegerreceiver

import (
	"context"

	"github.com/census-instrumentation/opencensus-service/cmd/occollector/app/builder"
	"github.com/census-instrumentation/opencensus-service/internal/collector/processor"
	"github.com/census-instrumentation/opencensus-service/receiver/jaeger"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// Run starts the OpenCensus receiver endpoint.
func Run(logger *zap.Logger, v *viper.Viper, spanProc processor.SpanProcessor) (func(), error) {
	rOpts, err := builder.NewJaegerReceiverCfg().InitFromViper(v)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	jtr, err := jaeger.New(ctx, rOpts.JaegerThriftTChannelPort, rOpts.JaegerThriftHTTPPort)
	if err != nil {
		return nil, err
	}

	ss := processor.WrapWithSpanSink("jaeger", spanProc)
	if err := jtr.StartTraceReception(ctx, ss); err != nil {
		return nil, err
	}

	logger.Info("OpenCensus receiver is running.",
		zap.Int("thrift-tchannel-port", rOpts.JaegerThriftTChannelPort),
		zap.Int("thrift-http-port", rOpts.JaegerThriftHTTPPort))

	closeFn := func() {
		jtr.StopTraceReception(context.Background())
	}

	return closeFn, nil
}

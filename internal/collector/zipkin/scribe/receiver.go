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

// Package zipkinscribereceiver wraps the functionality to start the end-point that
// receives Zipkin Scribe spans.
package zipkinscribereceiver

import (
	"context"
	"fmt"

	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/census-instrumentation/opencensus-service/internal/collector/processor"
	"github.com/census-instrumentation/opencensus-service/receiver"
	"github.com/census-instrumentation/opencensus-service/receiver/zipkinreceiver/scribe"
)

// Start starts the Zipkin Scribe receiver endpoint.
func Start(logger *zap.Logger, v *viper.Viper, spanProc processor.SpanProcessor) (receiver.TraceReceiver, error) {
	factory := scribe.Factory
	ss := processor.WrapWithSpanSink(factory.Type(), spanProc)
	sr, cfg, err := factory.NewFromViper(v, ss)
	if err != nil {
		return nil, fmt.Errorf("failed to create the %s trace receiver: %v", factory.Type(), err)
	}

	if err := sr.StartTraceReception(context.Background(), ss); err != nil {
		return nil, fmt.Errorf("cannot start %s receiver: %v", factory.Type(), err)
	}

	logger.Info("Receiver running", zap.String("type", factory.Type()), zap.Any("config", cfg))

	return sr, nil
}

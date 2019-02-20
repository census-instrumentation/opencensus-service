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

package processor

import (
	"context"

	"github.com/census-instrumentation/opencensus-service/data"
	"go.uber.org/zap"
)

// A debug processor that does not sends the data to any destination but logs debugging messages.
type debugTraceDataProcessor struct{ logger *zap.Logger }

var _ TraceDataProcessor = (*debugTraceDataProcessor)(nil)

func (sp *debugTraceDataProcessor) ProcessTraceData(ctx context.Context, td data.TraceData) error {
	sp.logger.Debug("debugTraceDataProcessor", zap.Int("#spans", len(td.Spans)))
	return nil
}

// NewDebugTraceDataProcessor creates an TraceDataProcessor that just drops the received data and logs debugging messages.
func NewDebugTraceDataProcessor(logger *zap.Logger) TraceDataProcessor {
	return &debugTraceDataProcessor{logger: logger}
}

// A debug processor that does not sends the data to any destination but logs debugging messages.
type debugMetricsDataProcessor struct{ logger *zap.Logger }

var _ MetricsDataProcessor = (*debugMetricsDataProcessor)(nil)

func (sp *debugMetricsDataProcessor) ProcessMetricsData(ctx context.Context, md data.MetricsData) error {
	sp.logger.Debug("debugMetricsDataProcessor", zap.Int("#metrics", len(md.Metrics)))
	return nil
}

// NewDebugMetricsDataProcessor creates an MetricsDataProcessor that just drops the received data and logs debugging messages.
func NewDebugMetricsDataProcessor(logger *zap.Logger) MetricsDataProcessor {
	return &debugMetricsDataProcessor{logger: logger}
}

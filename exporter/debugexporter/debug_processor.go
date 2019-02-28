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

package debugexporter

import (
	"context"

	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/exporter"
	"go.uber.org/zap"
)

// A debug processor that does not sends the data to any destination but logs debugging messages.
type debugExporter struct{ logger *zap.Logger }

var _ exporter.TraceDataExporter = (*debugExporter)(nil)
var _ exporter.MetricsDataExporter = (*debugExporter)(nil)

func (sp *debugExporter) ProcessTraceData(ctx context.Context, td data.TraceData) error {
	sp.logger.Debug("debugTraceDataProcessor", zap.Int("#spans", len(td.Spans)))
	return nil
}

func (sp *debugExporter) ProcessMetricsData(ctx context.Context, md data.MetricsData) error {
	sp.logger.Debug("debugMetricsDataProcessor", zap.Int("#metrics", len(md.Metrics)))
	return nil
}

func (sp *debugExporter) ExportFormat() string {
	return "debug"
}

// NewDebugTraceDataExporter creates an exporter.TraceDataExporter that just drops the
// received data and logs debugging messages.
func NewDebugTraceDataExporter(logger *zap.Logger) exporter.TraceDataExporter {
	return &debugExporter{logger: logger}
}

// NewDebugMetricsDataExporter creates an exporter.MetricsDataExporter that just drops the
// received data and logs debugging messages.
func NewDebugMetricsDataExporter(logger *zap.Logger) exporter.MetricsDataExporter {
	return &debugExporter{logger: logger}
}

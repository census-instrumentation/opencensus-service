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

package exportertest

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/exporter"
)

// SinkTraceExporter acts as a trace receiver for use in tests.
type SinkTraceExporter struct {
	mu     sync.Mutex
	traces []data.TraceData
}

var _ exporter.TraceExporter = (*SinkTraceExporter)(nil)

// ProcessTraceData stores traces for tests.
func (cts *SinkTraceExporter) ProcessTraceData(ctx context.Context, td data.TraceData) error {
	cts.mu.Lock()
	defer cts.mu.Unlock()

	cts.traces = append(cts.traces, td)

	return nil
}

const sinkExportFormat = "SinkExporter"

// ExportFormat retruns the name of this TraceExporter
func (cts *SinkTraceExporter) ExportFormat() string {
	return sinkExportFormat
}

// AllTraces returns the traces sent to the test sink.
func (cts *SinkTraceExporter) AllTraces() []data.TraceData {
	cts.mu.Lock()
	defer cts.mu.Unlock()

	return cts.traces[:]
}

// SinkMetricsExporter acts as a metrics receiver for use in tests.
type SinkMetricsExporter struct {
	mu      sync.Mutex
	metrics []data.MetricsData
}

var _ exporter.MetricsExporter = (*SinkMetricsExporter)(nil)

// ProcessMetricsData stores traces for tests.
func (cms *SinkMetricsExporter) ProcessMetricsData(ctx context.Context, md data.MetricsData) error {
	cms.mu.Lock()
	defer cms.mu.Unlock()

	cms.metrics = append(cms.metrics, md)

	return nil
}

// ExportFormat retruns the name of this TraceExporter
func (cms *SinkMetricsExporter) ExportFormat() string {
	return sinkExportFormat
}

// AllMetrics returns the metrics sent to the test sink.
func (cms *SinkMetricsExporter) AllMetrics() []data.MetricsData {
	cms.mu.Lock()
	defer cms.mu.Unlock()

	return cms.metrics[:]
}

// ToJSON marshals a generic interface to JSON to enable easy comparisons.
func ToJSON(v interface{}) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return b
}

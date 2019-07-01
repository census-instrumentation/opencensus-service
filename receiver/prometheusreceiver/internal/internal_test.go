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

package internal

import (
	"context"
	"errors"
	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/scrape"
	"go.uber.org/zap"
)

// test helpers

var zapLogger *zap.Logger
var testLogger *zap.SugaredLogger

func init() {
	zl, _ := zap.NewDevelopment()
	zapLogger = zl
	testLogger = zapLogger.Sugar()
}

func newMockMetadataCache(data map[string]scrape.MetricMetadata) *mockMetadataCache {
	return &mockMetadataCache{
		mCache: mCache{startValues: make(map[string]*ObservedValue)},
		data:   data,
	}
}

type mockMetadataCache struct {
	mCache
	data         map[string]scrape.MetricMetadata
	lastScrapeTs int64
}

func (m *mockMetadataCache) LoadObservedValue(key string) (*ObservedValue, bool) {
	return m.mCache.LoadObservedValue(key)
}

func (m *mockMetadataCache) StoreStartValue(key string, ts int64, value float64) {
	m.mCache.StoreObservedValue(key, ts, value)
}

func (m *mockMetadataCache) LastScrapeTime() int64 {
	return m.lastScrapeTs
}

func (m *mockMetadataCache) UpdateLastScrapeTime(ts int64) {
	m.lastScrapeTs = ts
}

func (m *mockMetadataCache) Metadata(metricName string) (scrape.MetricMetadata, bool) {
	mm, ok := m.data[metricName]
	return mm, ok
}

func (m *mockMetadataCache) SharedLabels() labels.Labels {
	return labels.FromStrings("__scheme__", "http")
}

func newMockConsumer() *mockConsumer {
	return &mockConsumer{}
}

type mockConsumer struct {
	md *data.MetricsData
}

func (m *mockConsumer) ConsumeMetricsData(ctx context.Context, md data.MetricsData) error {
	m.md = &md
	return nil
}

type mockMetadataSvc struct {
	caches map[string]*mockMetadataCache
}

func (mm *mockMetadataSvc) Get(job, instance string) (MetadataCache, error) {
	if mc, ok := mm.caches[job+"_"+instance]; ok {
		return mc, nil
	}

	return nil, errors.New("cache not found")
}

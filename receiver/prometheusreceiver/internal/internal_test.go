package internal

import (
	"context"
	"errors"
	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/scrape"
	"go.uber.org/zap"
	"sync"
)

// test helpers

var  zapLogger *zap.Logger
var  testLogger *zap.SugaredLogger

func init() {
	zl, _ := zap.NewDevelopment()
	zapLogger = zl
	testLogger = zapLogger.Sugar()
}

type mockMetadataCache struct {
	data map[string]scrape.MetricMetadata
}

func (m *mockMetadataCache) Metadata(metricName string) (scrape.MetricMetadata, bool) {
	mm, ok := m.data[metricName]
	return mm, ok
}

func (mc *mockMetadataCache) SharedLabels() labels.Labels  {
	return labels.Labels([]labels.Label{{"__scheme__", "http"}})
}


func NewMockConsumer() *mockConsumer {
	return &mockConsumer{
		Metrics: make(chan *data.MetricsData, 1),
	}
}

type mockConsumer struct {
	Metrics chan *data.MetricsData
	consumOnce sync.Once
}

func (m *mockConsumer) ConsumeMetricsData(ctx context.Context, md data.MetricsData) error {
	m.consumOnce.Do(func(){
		m.Metrics <- &md
	})
	return nil
}

type mockMetadataSvc struct {
	caches map[string]*mockMetadataCache
}

func (mm *mockMetadataSvc) Get(job, instance string) (MetadataCache, error) {
	if mc, ok:=mm.caches[job+"_"+instance];  ok {
		return mc, nil
	}

	return nil, errors.New("cache not found")
}
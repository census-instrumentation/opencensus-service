// Copyright 2020, OpenCensus Authors
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

package kairosdbexporter

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/census-instrumentation/opencensus-service/consumer"
	"github.com/census-instrumentation/opencensus-service/data"
	"go.uber.org/zap"

	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
)

// newExporter instantiates a new exporter with all required dependencies configured.
func newExporter(logger *zap.Logger, cfg *config) *exporter {
	httpTransport := &http.Transport{
		MaxIdleConns:       cfg.HTTPConfig.MaxConnections,
		IdleConnTimeout:    time.Duration(cfg.HTTPConfig.ConnectionTimeoutSeconds) * time.Second,
		DisableCompression: !cfg.HTTPConfig.UseCompression,
	}

	restClient := &restClient{
		logger:              logger,
		endpoint:            cfg.Endpoint,
		logProducedMessages: cfg.LogProducedMessages,
		httpTransport:       httpTransport,
		httpCli: &http.Client{
			Transport: httpTransport,
		},
	}

	return &exporter{
		logger:     logger,
		cfg:        cfg,
		restClient: restClient,
	}
}

// exporter provides the logic for shipping consumed metrics to KairosDB over the REST API.
type exporter struct {
	consumer.MetricsConsumer

	logger     *zap.Logger
	cfg        *config
	restClient *restClient
}

// ConsumeMetricsData provides the logic for shipping the received metrics to the TSDB over
// the REST api.
func (e *exporter) ConsumeMetricsData(ctx context.Context, md data.MetricsData) error {
	e.logger.Debug(fmt.Sprintf("Exporting %d metrics to kairosdb endpoint %s.", len(md.Metrics), e.cfg.Endpoint))

	if e.cfg.LogConsumedMetrics {
		e.logger.Debug(fmt.Sprintf("%v", md.Metrics))
	}

	collectedMetrics := e.toMetrics(md)
	e.logger.Debug(fmt.Sprintf("%v", collectedMetrics))

	return e.restClient.AddMetricsBuilder(ctx, collectedMetrics)
}

// toMetrics converts the opencensus metrics data to kairosdb metrics.
func (e *exporter) toMetrics(md data.MetricsData) *metricsBuilder {
	builder := newMetricsBuilder()

	for _, m := range md.Metrics {
		for _, timeSeries := range m.Timeseries {
			mb := newMetricBuilder().WithName(m.MetricDescriptor.Name)

			tagNames := []string{}
			for _, tagName := range m.MetricDescriptor.GetLabelKeys() {
				tagNames = append(tagNames, tagName.Key)
			}

			metricName := mb.Build().Name
			for idx, tagValue := range timeSeries.GetLabelValues() {
				tagName := tagNames[idx]

				if tagValue == nil || len(tagValue.Value) == 0 {
					e.logger.Debug(fmt.Sprintf("Skipping tag %s for metric %s because value is empty.",
						tagName, metricName))
					continue
				}
				mb = mb.AddTag(tagName, tagValue.Value)
			}

			for _, point := range timeSeries.Points {
				pointTime := time.Unix(point.Timestamp.GetSeconds(), int64(point.Timestamp.GetNanos()))
				pointValue := e.getPointValue(point)
				if pointValue == nil {
					errMsg := fmt.Sprintf("Unable to obtain point value for metric %s", metricName)
					e.logger.Error(errMsg)
					continue
				}

				if value, ok := pointValue.(int); ok {
					if value == 0 {
						e.logger.Debug(fmt.Sprintf("Skipping datapoint for metric %s because value is 0.", metricName))
						continue
					}
				}

				mb = mb.AddDataPoint(&datapointHelper{
					ts:    pointTime.UnixNano() / int64(time.Millisecond),
					value: pointValue,
				})

				builder = builder.AddMetricBuilder(mb)
			}
		}
	}

	return builder
}

// getPointValue converts complex opencensus value objects into numerical values.
func (e *exporter) getPointValue(p *metricspb.Point) interface{} {
	switch p.Value.(type) {
	case *metricspb.Point_DistributionValue:
		value := p.GetDistributionValue().Sum / float64(p.GetDistributionValue().Count)
		if value != 0 {
			return value
		}
	case *metricspb.Point_DoubleValue:
		if p.GetDoubleValue() != 0 {
			return p.GetDoubleValue()
		}
	case *metricspb.Point_Int64Value:
		if p.GetInt64Value() != 0 {
			return p.GetInt64Value()
		}
	case *metricspb.Point_SummaryValue:
		return p.GetSummaryValue().Sum.Value / float64(p.GetSummaryValue().Count.Value)
	default:
		return nil
	}

	return 0
}

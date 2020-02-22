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

import "fmt"

// datapoint defines a tuple which can be ingested in KairosDB using the Rest API.
// usually is (timestamp, value) where value might have different values.
type datapoint [2]interface{}

// datapointHelper provides a very simple helper structure in order to define data points
// in a more controllable manner.
type datapointHelper struct {
	ts    int64
	value interface{}
}

// Datapoint converts the current helper into a datapoint tuple.
func (p *datapointHelper) Datapoint() datapoint {
	return datapoint([2]interface{}{
		p.ts,
		p.value,
	})
}

// tags provides a model for defining arbitrary tags which must be stored with the metric.
type tags map[string]string

// metricType provides a specific type which can be used to determine the concrete internal type
// used for metric data point value.
type metricType string

// metric defines the model which can be used to ingest entries in the KairosDB.
// The model is designed based on https://kairosdb.github.io/docs/build/html/restapi/AddDataPoints.html
// specification.
type metric struct {
	Name       string      `json:"name"`
	Datapoints []datapoint `json:"datapoints"`
	Tags       tags        `json:"tags"`
	Type       metricType  `json:"type,omitempty"`
}

func (m *metric) String() string {
	return fmt.Sprintf("Name=%s Type=%s Tags=%v Datapoints=%v", m.Name, m.Type, m.Tags, m.Datapoints)
}

// metrics defines a concrete model for working with collection of metrics.
type metrics []*metric

func newMetricBuilder() *metricBuilder {
	return &metricBuilder{m: &metric{
		Datapoints: []datapoint{},
		Tags:       tags{},
	}}
}

func newMetricsBuilder() *metricsBuilder {
	return &metricsBuilder{
		metricsCollection: metrics{},
	}
}

// metricBuilder provides a useful structure for constructing a specific metric using a fluent
// interface.
type metricBuilder struct {
	m *metric
}

func (b *metricBuilder) WithName(name string) *metricBuilder {
	b.m.Name = name
	return b
}

func (b *metricBuilder) AddDataPoint(p *datapointHelper) *metricBuilder {
	b.m.Datapoints = append(b.m.Datapoints, p.Datapoint())
	return b
}

func (b *metricBuilder) AddTag(name string, value string) *metricBuilder {
	b.m.Tags[name] = value
	return b
}

func (b *metricBuilder) Build() *metric {
	return b.m
}

// metricsBuilder provides a useful structure for constructing collection of metrics using a fluent
// interface.
type metricsBuilder struct {
	metricsCollection metrics
}

func (b *metricsBuilder) AddMetricBuilder(mb *metricBuilder) *metricsBuilder {
	return b.AddMetric(mb.Build())
}

func (b *metricsBuilder) AddMetric(m *metric) *metricsBuilder {
	b.metricsCollection = append(b.metricsCollection, m)
	return b
}

func (b *metricsBuilder) Build() metrics {
	return b.metricsCollection
}

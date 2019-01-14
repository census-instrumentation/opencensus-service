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

package exporterparser

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/exporter"

	// TODO: once this repository has been transferred to the
	// official census-ecosystem location, update this import path.
	"github.com/orijtech/prometheus-go-metrics-exporter"

	prometheus_golang "github.com/prometheus/client_golang/prometheus"
)

type prometheusConfig struct {
	// Namespace if set, exports metrics under the provided value.
	Namespace string `yaml:"namespace"`

	// ConstLabels are values that are applied for every exported metric.
	ConstLabels prometheus_golang.Labels `yaml:"const_labels"`

	// The address on which the Prometheus scrape handler will be run on.
	Address string `yaml:"address"`
}

var errBlankPrometheusAddress = errors.New("expecting a non-blank address to run the Prometheus metrics handler")

// PrometheusExportersFromYAML parses the yaml bytes and returns exporter.MetricsExporters
// targeting Prometheus according to the configuration settings.
// It allows HTTP clients to scrape it on endpoint path "/metrics".
func PrometheusExportersFromYAML(config []byte) (tes []exporter.TraceExporter, mes []exporter.MetricsExporter, doneFns []func() error, err error) {
	var cfg struct {
		Exporters *struct {
			Prometheus *prometheusConfig `yaml:"prometheus"`
		} `yaml:"exporters"`
	}
	if err := yamlUnmarshal(config, &cfg); err != nil {
		return nil, nil, nil, err
	}
	if cfg.Exporters == nil || cfg.Exporters.Prometheus == nil {
		return nil, nil, nil, nil
	}

	pcfg := cfg.Exporters.Prometheus
	addr := strings.TrimSpace(pcfg.Address)
	if addr == "" {
		err = errBlankPrometheusAddress
		return
	}

	opts := prometheus.Options{
		Namespace:   pcfg.Namespace,
		ConstLabels: pcfg.ConstLabels,
	}
	pe, err := prometheus.New(opts)
	if err != nil {
		return nil, nil, nil, err
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, nil, err
	}

	// The Prometheus metrics exporter has to run on the provided address
	// as a server that'll be scraped by Prometheus.
	mux := http.NewServeMux()
	mux.Handle("/metrics", pe)

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(ln)
	}()

	doneFns = append(doneFns, ln.Close)
	pexp := &prometheusExporter{exporter: pe}
	mes = append(mes, pexp)

	return
}

type prometheusExporter struct {
	exporter *prometheus.Exporter
}

var _ exporter.MetricsExporter = (*prometheusExporter)(nil)

func (pe *prometheusExporter) ExportMetricsData(ctx context.Context, md data.MetricsData) error {
	for _, metric := range md.Metrics {
		_ = pe.exporter.ExportMetric(ctx, md.Node, md.Resource, metric)
	}
	return nil
}

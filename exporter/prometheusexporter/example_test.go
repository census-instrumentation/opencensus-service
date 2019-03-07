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

package prometheusexporter_test

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/census-instrumentation/opencensus-service/exporter/prometheusexporter"

	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
	"github.com/golang/protobuf/ptypes/timestamp"
)

func Example() {
	pe, err := prometheusexporter.New(prometheusexporter.Options{})
	if err != nil {
		log.Fatalf("Failed to create new exporter: %v", err)
	}

	mux := http.NewServeMux()
	// Expose the Prometheus Metrics exporter for scraping on route /metrics.
	mux.Handle("/metrics", pe)

	// Now run the server.
	go func() {
		http.ListenAndServe(":8888", mux)
	}()

	// And finally in your client application, use the
	// OpenCensus-Go Metrics Prometheus exporter to record metrics.
	for {
		pe.ExportMetric(context.Background(), nil, nil, metric1)
		// Introducing a fake pause/period.
		<-time.After(350 * time.Millisecond)
	}
}

var (
	startTimestamp = &timestamp.Timestamp{
		Seconds: 1543160298,
		Nanos:   100000090,
	}
	endTimestamp = &timestamp.Timestamp{
		Seconds: 1543160298,
		Nanos:   100000997,
	}
)

var metric1 = &metricspb.Metric{
	Descriptor_: &metricspb.Metric_MetricDescriptor{
		MetricDescriptor: &metricspb.MetricDescriptor{
			Name:        "this/one/there(where)",
			Description: "Extra ones",
			Unit:        "1",
			LabelKeys: []*metricspb.LabelKey{
				{Key: "os", Description: "Operating system"},
				{Key: "arch", Description: "Architecture"},
			},
		},
	},
	Timeseries: []*metricspb.TimeSeries{
		{
			StartTimestamp: startTimestamp,
			LabelValues: []*metricspb.LabelValue{
				{Value: "windows"},
				{Value: "x86"},
			},
			Points: []*metricspb.Point{
				{
					Timestamp: endTimestamp,
					Value: &metricspb.Point_Int64Value{
						Int64Value: 99,
					},
				},
			},
		},
		{
			StartTimestamp: startTimestamp,
			LabelValues: []*metricspb.LabelValue{
				{Value: "darwin"},
				{Value: "386"},
			},
			Points: []*metricspb.Point{
				{
					Timestamp: endTimestamp,
					Value: &metricspb.Point_DoubleValue{
						DoubleValue: 49.5,
					},
				},
			},
		},
	},
}

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

package prometheusexporter

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
	"github.com/golang/protobuf/ptypes/timestamp"

	"github.com/prometheus/client_golang/prometheus"
)

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

func TestOnlyCumulativeWindowSupported(t *testing.T) {

	tests := []struct {
		metric    *metricspb.Metric
		wantCount int
	}{
		{
			metric: &metricspb.Metric{}, wantCount: 0,
		},
		{
			metric: &metricspb.Metric{
				Descriptor_: &metricspb.Metric_MetricDescriptor{
					MetricDescriptor: &metricspb.MetricDescriptor{
						Name:        "with_metric_descriptor",
						Description: "This is a test",
						Unit:        "By",
					},
				},
				Timeseries: []*metricspb.TimeSeries{
					{
						StartTimestamp: startTimestamp,
						Points: []*metricspb.Point{
							{
								Timestamp: endTimestamp,
								Value: &metricspb.Point_DistributionValue{
									DistributionValue: &metricspb.DistributionValue{
										Count:                 1,
										Sum:                   11.9,
										SumOfSquaredDeviation: 0,
										Buckets: []*metricspb.DistributionValue_Bucket{
											{}, {Count: 1}, {}, {}, {},
										},
										BucketOptions: &metricspb.DistributionValue_BucketOptions{
											Type: &metricspb.DistributionValue_BucketOptions_Explicit_{
												Explicit: &metricspb.DistributionValue_BucketOptions_Explicit{
													Bounds: []float64{0, 10, 20, 30, 40},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantCount: 1,
		},
		{
			metric: &metricspb.Metric{
				Descriptor_: &metricspb.Metric_MetricDescriptor{
					MetricDescriptor: &metricspb.MetricDescriptor{
						Name:        "counter",
						Description: "This is a counter",
						Unit:        "1",
					},
				},
				Timeseries: []*metricspb.TimeSeries{
					{
						StartTimestamp: startTimestamp,
						Points: []*metricspb.Point{
							{
								Timestamp: endTimestamp,
								Value:     &metricspb.Point_Int64Value{Int64Value: 197},
							},
						},
					},
				},
			},
			wantCount: 1,
		},
	}

	for i, tt := range tests {
		reg := prometheus.NewRegistry()
		collector := newCollector(Options{}, reg)
		collector.addMetric(tt.metric)
		mm, err := reg.Gather()
		if err != nil {
			t.Errorf("#%d: Gather error: %v", i, err)
		}
		reg.Unregister(collector)
		if got, want := len(mm), tt.wantCount; got != want {
			t.Errorf("#%d: Got %d Want %d", i, got, want)
		}
	}
}

func TestCollectNonRacy(t *testing.T) {
	exp, err := New(Options{})
	if err != nil {
		t.Fatalf("NewExporter failed: %v", err)
	}
	collector := exp.collector

	// Synchronization to ensure that every goroutine terminates before we exit.
	var waiter sync.WaitGroup
	waiter.Add(3)
	defer waiter.Wait()

	doneCh := make(chan bool)

	// 1. Simulate metrics write route with a period of 700ns.
	go func() {
		defer waiter.Done()
		tick := time.NewTicker(700 * time.Nanosecond)

		defer func() {
			tick.Stop()
			close(doneCh)
		}()

		for i := 0; i < 1e3; i++ {
			metrics := []*metricspb.Metric{
				{
					Descriptor_: &metricspb.Metric_MetricDescriptor{
						MetricDescriptor: &metricspb.MetricDescriptor{
							Name:        "with_metric_descriptor",
							Description: "This is a test",
							Unit:        "By",
						},
					},
					Timeseries: []*metricspb.TimeSeries{
						{
							StartTimestamp: startTimestamp,
							Points: []*metricspb.Point{
								{
									Timestamp: endTimestamp,
									Value: &metricspb.Point_DistributionValue{
										DistributionValue: &metricspb.DistributionValue{
											Count:                 int64(i + 10),
											Sum:                   11.9 + float64(i),
											SumOfSquaredDeviation: 0,
											Buckets: []*metricspb.DistributionValue_Bucket{
												{}, {Count: 1}, {}, {}, {},
											},
											BucketOptions: &metricspb.DistributionValue_BucketOptions{
												Type: &metricspb.DistributionValue_BucketOptions_Explicit_{
													Explicit: &metricspb.DistributionValue_BucketOptions_Explicit{
														Bounds: []float64{0, 10, 20, 30, 40},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				{
					Descriptor_: &metricspb.Metric_MetricDescriptor{
						MetricDescriptor: &metricspb.MetricDescriptor{
							Name:        "counter",
							Description: "This is a counter",
							Unit:        "1",
						},
					},
					Timeseries: []*metricspb.TimeSeries{
						{
							StartTimestamp: startTimestamp,
							Points: []*metricspb.Point{
								{
									Timestamp: endTimestamp,
									Value:     &metricspb.Point_Int64Value{Int64Value: int64(i)},
								},
							},
						},
					},
				},
			}

			for _, metric := range metrics {
				if err := exp.ExportMetric(context.Background(), nil, nil, metric); err != nil {
					t.Errorf("Iteration #%d:: unexpected ExportMetric error: %v", i, err)
				}
				<-tick.C
			}
		}
	}()

	inMetricsChan := make(chan prometheus.Metric, 1000)
	// 2. Simulate the Prometheus metrics consumption routine running at 900ns.
	go func() {
		defer waiter.Done()
		tick := time.NewTicker(900 * time.Nanosecond)
		defer tick.Stop()

		for {
			select {
			case <-doneCh:
				return

			case <-inMetricsChan:
			case <-tick.C:
			}
		}
	}()

	// 3. Collect/Read routine running at 800ns.
	go func() {
		defer waiter.Done()
		tick := time.NewTicker(800 * time.Nanosecond)
		defer tick.Stop()

		for {
			select {
			case <-doneCh:
				return

			case <-tick.C:
				// Perform some collection here.
				collector.Collect(inMetricsChan)
			}
		}
	}()
}

func makeMetrics() []*metricspb.Metric {
	return []*metricspb.Metric{
		{
			Descriptor_: &metricspb.Metric_MetricDescriptor{
				MetricDescriptor: &metricspb.MetricDescriptor{
					Name:        "with/metric*descriptor",
					Description: "This is a test",
					Unit:        "By",
				},
			},
			Timeseries: []*metricspb.TimeSeries{
				{
					StartTimestamp: startTimestamp,
					Points: []*metricspb.Point{
						{
							Timestamp: endTimestamp,
							Value: &metricspb.Point_DistributionValue{
								DistributionValue: &metricspb.DistributionValue{
									Count:                 2,
									Sum:                   61.9,
									SumOfSquaredDeviation: 0,
									Buckets: []*metricspb.DistributionValue_Bucket{
										{}, {Count: 1}, {}, {}, {Count: 5},
									},
									BucketOptions: &metricspb.DistributionValue_BucketOptions{
										Type: &metricspb.DistributionValue_BucketOptions_Explicit_{
											Explicit: &metricspb.DistributionValue_BucketOptions_Explicit{
												Bounds: []float64{0, 10, 20, 30, 40},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			Descriptor_: &metricspb.Metric_MetricDescriptor{
				MetricDescriptor: &metricspb.MetricDescriptor{
					Name:        "this/one/there(where)",
					Description: "Extra ones",
					Unit:        "1",
					LabelKeys: []*metricspb.LabelKey{
						{Key: "os", Description: "Operating system"},
						{Key: "arch", Description: "Architecture"},
						{Key: "my.org/department", Description: "The department that owns this server"},
					},
				},
			},
			Timeseries: []*metricspb.TimeSeries{
				{
					StartTimestamp: startTimestamp,
					LabelValues: []*metricspb.LabelValue{
						{Value: "windows"},
						{Value: "x86"},
						{Value: "Storage"},
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
						{Value: "Ops"},
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
		},
	}
}

func TestMetricsEndpointOutput(t *testing.T) {
	exp, err := New(Options{})
	if err != nil {
		t.Fatalf("Failed to create Prometheus exporter: %v", err)
	}

	srv := httptest.NewServer(exp)
	defer srv.Close()

	// Now record some metrics.
	metrics := makeMetrics()
	for _, metric := range metrics {
		exp.ExportMetric(context.Background(), nil, nil, metric)
	}

	var i int
	var output string
	for {
		time.Sleep(10 * time.Millisecond)
		if i == 1000 {
			t.Fatal("no output at / (10s wait)")
		}
		i++

		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Fatalf("Failed to get metrics on / error: %v", err)
		}

		slurp, err := ioutil.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("Failed to read body: %v", err)
		}

		output = string(slurp)
		if output != "" {
			break
		}
	}

	if strings.Contains(output, "collected before with the same name and label values") {
		t.Fatalf("metric name and labels were duplicated but must be unique. Got\n\t%q", output)
	}

	if strings.Contains(output, "error(s) occurred") {
		t.Fatalf("error reported by Prometheus registry:\n\t%s", output)
	}

	want := `# HELP this_one_there_where_ Extra ones
# TYPE this_one_there_where_ counter
this_one_there_where_{arch="386",my_org_department="Ops",os="darwin"} 49.5
this_one_there_where_{arch="x86",my_org_department="Storage",os="windows"} 99
# HELP with_metric_descriptor This is a test
# TYPE with_metric_descriptor histogram
with_metric_descriptor_bucket{le="0"} 0
with_metric_descriptor_bucket{le="10"} 1
with_metric_descriptor_bucket{le="20"} 1
with_metric_descriptor_bucket{le="30"} 1
with_metric_descriptor_bucket{le="40"} 6
with_metric_descriptor_bucket{le="+Inf"} 2
with_metric_descriptor_sum 61.9
with_metric_descriptor_count 2
`
	if g, w := output, want; g != w {
		t.Errorf("Mismatched output\nGot:\n%s\nWant:\n%s", g, w)
	}
}

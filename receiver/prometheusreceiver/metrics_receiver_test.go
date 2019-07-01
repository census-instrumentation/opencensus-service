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

package prometheusreceiver

import (
	"context"
	"fmt"
	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/exporter/exportertest"
	"github.com/census-instrumentation/opencensus-service/internal/config/viperutils"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

var logger, _ = zap.NewDevelopment()

type mockPrometheusResponse struct {
	code int
	data string
}

type mockPrometheus struct {
	endpoints   map[string][]mockPrometheusResponse
	accessIndex map[string]*int32
	wg          *sync.WaitGroup
}

func newMockPrometheus(endpoints map[string][]mockPrometheusResponse) *mockPrometheus {
	accessIndex := make(map[string]*int32)
	wg := &sync.WaitGroup{}
	wg.Add(len(endpoints))
	for k := range endpoints {
		v := int32(0)
		accessIndex[k] = &v
	}
	return &mockPrometheus{
		wg:          wg,
		accessIndex: accessIndex,
		endpoints:   endpoints,
	}
}

func (mp *mockPrometheus) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	iptr, ok := mp.accessIndex[req.URL.Path]
	if !ok {
		rw.WriteHeader(404)
		return
	}
	index := int(*iptr)
	atomic.AddInt32(iptr, 1)
	pages := mp.endpoints[req.URL.Path]
	if index >= len(pages) {
		if index == len(pages) {
			mp.wg.Done()
		}
		rw.WriteHeader(404)
		return
	}
	rw.WriteHeader(pages[index].code)
	_, _ = rw.Write([]byte(pages[index].data))
}

func TestNew(t *testing.T) {
	v := viper.New()

	_, err := New(logger, v, nil)
	if err != errNilScrapeConfig {
		t.Fatalf("Expected errNilScrapeConfig but did not get it.")
	}

	v.Set("config", nil)
	_, err = New(logger, v, nil)
	if err != errNilScrapeConfig {
		t.Fatalf("Expected errNilScrapeConfig but did not get it.")
	}

	v.Set("config.blah", "some_value")
	_, err = New(logger, v, nil)
	if err != errNilScrapeConfig {
		t.Fatalf("Expected errNilScrapeConfig but did not get it.")
	}
}

// Test data for EndToEnd test

var target1Page1 = `
# HELP go_threads Number of OS threads created
# TYPE go_threads gauge
go_threads 19

# HELP http_requests_total The total number of HTTP requests.
# TYPE http_requests_total counter
http_requests_total{method="post",code="200"} 100
http_requests_total{method="post",code="400"} 5

# HELP http_request_duration_seconds A histogram of the request duration.
# TYPE http_request_duration_seconds histogram
http_request_duration_seconds_bucket{le="0.05"} 1000
http_request_duration_seconds_bucket{le="0.5"} 1500
http_request_duration_seconds_bucket{le="1"} 2000
http_request_duration_seconds_bucket{le="+Inf"} 2500
http_request_duration_seconds_sum 5000
http_request_duration_seconds_count 2500

# HELP rpc_duration_seconds A summary of the RPC duration in seconds.
# TYPE rpc_duration_seconds summary
rpc_duration_seconds{quantile="0.01"} 1
rpc_duration_seconds{quantile="0.9"} 5
rpc_duration_seconds{quantile="0.99"} 8
rpc_duration_seconds_sum 5000
rpc_duration_seconds_count 1000
`

var target1Page2 = `
# HELP go_threads Number of OS threads created
# TYPE go_threads gauge
go_threads 18

# HELP http_requests_total The total number of HTTP requests.
# TYPE http_requests_total counter
http_requests_total{method="post",code="200"} 199
http_requests_total{method="post",code="400"} 12
http_requests_total{method="post",code="500"} 3

# HELP http_request_duration_seconds A histogram of the request duration.
# TYPE http_request_duration_seconds histogram
http_request_duration_seconds_bucket{le="0.05"} 1100
http_request_duration_seconds_bucket{le="0.5"} 1600
http_request_duration_seconds_bucket{le="1"} 2100
http_request_duration_seconds_bucket{le="+Inf"} 2600
http_request_duration_seconds_sum 5050
http_request_duration_seconds_count 2600

# HELP rpc_duration_seconds A summary of the RPC duration in seconds.
# TYPE rpc_duration_seconds summary
rpc_duration_seconds{quantile="0.01"} 1
rpc_duration_seconds{quantile="0.9"} 5
rpc_duration_seconds{quantile="0.99"} 8
rpc_duration_seconds_sum 5002
rpc_duration_seconds_count 1001
`

var target2Page1 = `
# HELP go_threads Number of OS threads created
# TYPE go_threads gauge
go_threads 18

# HELP http_requests_total The total number of HTTP requests.
# TYPE http_requests_total counter
http_requests_total{method="post",code="200"} 10
http_requests_total{method="post",code="400"} 50
`

var target2Page2 = `
# HELP go_threads Number of OS threads created
# TYPE go_threads gauge
go_threads 16

# HELP http_requests_total The total number of HTTP requests.
# TYPE http_requests_total counter
http_requests_total{method="post",code="200"} 50
http_requests_total{method="post",code="400"} 60
http_requests_total{method="post",code="500"} 3
`

var scrapeConfig = `
config:
  scrape_configs:
    - job_name: 'target1'
      scrape_interval: 1s
      metrics_path: /target1/metrics
      static_configs:
        - targets: ['%s']
    - job_name: 'target2'
      scrape_interval: 1s
      metrics_path: /target2/metrics
      static_configs:
        - targets: ['%s']
`

var scrapeConfig1 = `
config:
  scrape_configs:
    - job_name: 'target1'
      scrape_interval: 1s
      metrics_path: /target1/metrics
      static_configs:
        - targets: ['%s']
`

func TestEndToEnd(t *testing.T) {
	// 1. setup input data and mock server
	endpoints := make(map[string][]mockPrometheusResponse)
	endpoints["/target1/metrics"] = []mockPrometheusResponse{
		{code: 200, data: target1Page1},
		{code: 500, data: ""},
		{code: 200, data: target1Page2},
	}
	endpoints["/target2/metrics"] = []mockPrometheusResponse{
		{code: 200, data: target2Page1},
		{code: 200, data: target2Page2},
	}

	mp := newMockPrometheus(endpoints)
	cst := httptest.NewServer(mp)
	defer cst.Close()
	cstURL, _ := url.Parse(cst.URL)
	host, port, _ := net.SplitHostPort(cstURL.Host)

	// 2. setup reciver and sinkexporter
	yamlConfig := fmt.Sprintf(scrapeConfig, cstURL.Host, cstURL.Host)

	v := viper.New()
	if err := viperutils.LoadYAMLBytes(v, []byte(yamlConfig)); err != nil {
		t.Fatalf("Failed to load yaml config into viper")
	}

	cms := new(exportertest.SinkMetricsExporter)
	precv, err := New(logger, v, cms)
	if err != nil {
		t.Fatalf("Failed to create promreceiver: %v", err)
	}

	if err := precv.StartMetricsReception(context.Background(), nil); err != nil {
		t.Fatalf("Failed to invoke StartMetricsReception: %v", err)
	}
	defer precv.StopMetricsReception(context.Background())

	// wait for all provided data to be scraped
	mp.wg.Wait()

	metrics := cms.AllMetrics()
	//fmt.Println("len of metrics", len(metrics))
	//for _, m:=range metrics {
	//	fmt.Println(string(exportertest.ToJSON(m)))
	//}

	// split and store results by target name
	results := make(map[string][]data.MetricsData)
	for _, m := range metrics {
		result, ok := results[m.Node.ServiceInfo.Name]
		if !ok {
			result = make([]data.MetricsData, 0)
		}
		results[m.Node.ServiceInfo.Name] = append(result, m)
	}

	t.Run("shall-return-two-targets", func(t *testing.T) {
		if l := len(results); l != 2 {
			t.Errorf("want 2 targets, but got %v\n", l)
		}
	})

	t.Run("verify-target1-results", func(t *testing.T) {
		mds := results["target1"]
		// input has 3 pages, however, the 2nd one is 500 error,
		// expecting two metricData to be produced
		if l := len(mds); l != 2 {
			t.Errorf("want 2, but got %v\n", l)
		}
		m1 := mds[0]
		// m1 shall only have a gauge
		if l := len(m1.Metrics); l != 1 {
			t.Errorf("want 1, but got %v\n", l)
		}

		// only gauge value is returned from the first scrape
		wantG1 := &metricspb.Metric{
			MetricDescriptor: &metricspb.MetricDescriptor{
				Name:        "go_threads",
				Description: "Number of OS threads created",
				Type:        metricspb.MetricDescriptor_GAUGE_DOUBLE,
				LabelKeys:   []*metricspb.LabelKey{}},
			Timeseries: []*metricspb.TimeSeries{
				{
					LabelValues: []*metricspb.LabelValue{},
					Points: []*metricspb.Point{
						{Value: &metricspb.Point_DoubleValue{DoubleValue: 19.0}},
					},
				},
			},
		}
		gotG1 := m1.Metrics[0]
		ts1 := gotG1.Timeseries[0].Points[0].Timestamp
		// set this timestamp to wantG1
		wantG1.Timeseries[0].Points[0].Timestamp = ts1
		doCompare(t, wantG1, gotG1)

		// verify the 2nd metricData
		m2 := mds[1]
		ts2 := m2.Metrics[0].Timeseries[0].Points[0].Timestamp

		// ts for metric: `http_requests_total{method="post",code="500"} 3` since this metric is newly created from
		// the 2nd successful response, however, there's a 500 response inbetween, the startTimestamp of this metric
		// will be the ts from 2nd scrape even if it failed
		tsHTTPReqCounter500 := m2.Metrics[1].Timeseries[2].StartTimestamp
		if !(toUnixNano(ts1) < toUnixNano(tsHTTPReqCounter500) && toUnixNano(tsHTTPReqCounter500) < toUnixNano(ts2)) {
			t.Errorf("expect %#v  < %#v < %#v", ts1, tsHTTPReqCounter500, ts2)
		}

		want2 := &data.MetricsData{
			Node: &commonpb.Node{
				Identifier: &commonpb.ProcessIdentifier{
					HostName: host,
				},
				ServiceInfo: &commonpb.ServiceInfo{
					Name: "target1",
				},
				Attributes: map[string]string{
					"scheme": "http",
					"port":   port,
				},
			},
			Metrics: []*metricspb.Metric{
				{
					MetricDescriptor: &metricspb.MetricDescriptor{
						Name:        "go_threads",
						Description: "Number of OS threads created",
						Type:        metricspb.MetricDescriptor_GAUGE_DOUBLE,
						LabelKeys:   []*metricspb.LabelKey{}},
					Timeseries: []*metricspb.TimeSeries{
						{
							LabelValues: []*metricspb.LabelValue{},
							Points: []*metricspb.Point{
								{Timestamp: ts2, Value: &metricspb.Point_DoubleValue{DoubleValue: 18.0}},
							},
						},
					},
				},
				{
					MetricDescriptor: &metricspb.MetricDescriptor{
						Name:        "http_requests_total",
						Description: "The total number of HTTP requests.",
						Type:        metricspb.MetricDescriptor_CUMULATIVE_DOUBLE,
						LabelKeys:   []*metricspb.LabelKey{{Key: "code"}, {Key: "method"}},
					},
					Timeseries: []*metricspb.TimeSeries{
						{
							StartTimestamp: ts1,
							LabelValues: []*metricspb.LabelValue{
								{Value: "200", HasValue: true},
								{Value: "post", HasValue: true},
							},
							Points: []*metricspb.Point{
								{Timestamp: ts2, Value: &metricspb.Point_DoubleValue{DoubleValue: 99.0}},
							},
						},
						{
							StartTimestamp: ts1,
							LabelValues: []*metricspb.LabelValue{
								{Value: "400", HasValue: true},
								{Value: "post", HasValue: true},
							},
							Points: []*metricspb.Point{
								{Timestamp: ts2, Value: &metricspb.Point_DoubleValue{DoubleValue: 7.0}},
							},
						},
						{
							StartTimestamp: tsHTTPReqCounter500,
							LabelValues: []*metricspb.LabelValue{
								{Value: "500", HasValue: true},
								{Value: "post", HasValue: true},
							},
							Points: []*metricspb.Point{
								{Timestamp: ts2, Value: &metricspb.Point_DoubleValue{DoubleValue: 3.0}},
							},
						},
					},
				},
				{
					MetricDescriptor: &metricspb.MetricDescriptor{
						Name:        "http_request_duration_seconds",
						Type:        metricspb.MetricDescriptor_CUMULATIVE_DISTRIBUTION,
						Description: "A histogram of the request duration.",
						Unit:        "s",
						LabelKeys:   []*metricspb.LabelKey{}},
					Timeseries: []*metricspb.TimeSeries{
						{
							StartTimestamp: ts1,
							LabelValues:    []*metricspb.LabelValue{},
							Points: []*metricspb.Point{
								{
									Timestamp: ts2,
									Value: &metricspb.Point_DistributionValue{
										DistributionValue: &metricspb.DistributionValue{
											BucketOptions: &metricspb.DistributionValue_BucketOptions{
												Type: &metricspb.DistributionValue_BucketOptions_Explicit_{
													Explicit: &metricspb.DistributionValue_BucketOptions_Explicit{
														Bounds: []float64{0.05, 0.5, 1},
													},
												},
											},
											Count: 100,
											Sum:   50.0,
											Buckets: []*metricspb.DistributionValue_Bucket{
												{Count: 100},
												{Count: 0},
												{Count: 0},
												{Count: 0},
											},
										}},
								},
							},
						},
					},
				},
				{
					MetricDescriptor: &metricspb.MetricDescriptor{
						Name:        "rpc_duration_seconds",
						Type:        metricspb.MetricDescriptor_SUMMARY,
						Description: "A summary of the RPC duration in seconds.",
						Unit:        "s",
						LabelKeys:   []*metricspb.LabelKey{}},
					Timeseries: []*metricspb.TimeSeries{
						{
							StartTimestamp: ts1,
							LabelValues:    []*metricspb.LabelValue{},
							Points: []*metricspb.Point{
								{
									Timestamp: ts2, Value: &metricspb.Point_SummaryValue{
										SummaryValue: &metricspb.SummaryValue{
											Sum:   &wrappers.DoubleValue{Value: 2.0},
											Count: &wrappers.Int64Value{Value: 1},
											Snapshot: &metricspb.SummaryValue_Snapshot{
												PercentileValues: []*metricspb.SummaryValue_Snapshot_ValueAtPercentile{
													{Percentile: 1.0, Value: 1},
													{Percentile: 90.0, Value: 5},
													{Percentile: 99.0, Value: 8},
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
		}

		doCompare(t, want2, &m2)
	})

	t.Run("verify-target2-results", func(t *testing.T) {
		mds := results["target2"]
		// input has 3 pages, however, the 2nd one is 500 error,
		// expecting two metricData to be produced
		if l := len(mds); l != 2 {
			t.Errorf("want 2, but got %v\n", l)
		}
		m1 := mds[0]
		// m1 shall only have a gauge
		if l := len(m1.Metrics); l != 1 {
			t.Errorf("want 1, but got %v\n", l)
		}

		// only gauge value is returned from the first scrape
		wantG1 := &metricspb.Metric{
			MetricDescriptor: &metricspb.MetricDescriptor{
				Name:        "go_threads",
				Description: "Number of OS threads created",
				Type:        metricspb.MetricDescriptor_GAUGE_DOUBLE,
				LabelKeys:   []*metricspb.LabelKey{}},
			Timeseries: []*metricspb.TimeSeries{
				{
					LabelValues: []*metricspb.LabelValue{},
					Points: []*metricspb.Point{
						{Value: &metricspb.Point_DoubleValue{DoubleValue: 18.0}},
					},
				},
			},
		}
		gotG1 := m1.Metrics[0]
		ts1 := gotG1.Timeseries[0].Points[0].Timestamp
		// set this timestamp to wantG1
		wantG1.Timeseries[0].Points[0].Timestamp = ts1
		doCompare(t, wantG1, gotG1)

		// verify the 2nd metricData
		m2 := mds[1]
		ts2 := m2.Metrics[0].Timeseries[0].Points[0].Timestamp

		want2 := &data.MetricsData{
			Node: &commonpb.Node{
				Identifier: &commonpb.ProcessIdentifier{
					HostName: host,
				},
				ServiceInfo: &commonpb.ServiceInfo{
					Name: "target2",
				},
				Attributes: map[string]string{
					"scheme": "http",
					"port":   port,
				},
			},
			Metrics: []*metricspb.Metric{
				{
					MetricDescriptor: &metricspb.MetricDescriptor{
						Name:        "go_threads",
						Description: "Number of OS threads created",
						Type:        metricspb.MetricDescriptor_GAUGE_DOUBLE,
						LabelKeys:   []*metricspb.LabelKey{}},
					Timeseries: []*metricspb.TimeSeries{
						{
							LabelValues: []*metricspb.LabelValue{},
							Points: []*metricspb.Point{
								{Timestamp: ts2, Value: &metricspb.Point_DoubleValue{DoubleValue: 16.0}},
							},
						},
					},
				},
				{
					MetricDescriptor: &metricspb.MetricDescriptor{
						Name:        "http_requests_total",
						Description: "The total number of HTTP requests.",
						Type:        metricspb.MetricDescriptor_CUMULATIVE_DOUBLE,
						LabelKeys:   []*metricspb.LabelKey{{Key: "code"}, {Key: "method"}},
					},
					Timeseries: []*metricspb.TimeSeries{
						{
							StartTimestamp: ts1,
							LabelValues: []*metricspb.LabelValue{
								{Value: "200", HasValue: true},
								{Value: "post", HasValue: true},
							},
							Points: []*metricspb.Point{
								{Timestamp: ts2, Value: &metricspb.Point_DoubleValue{DoubleValue: 40.0}},
							},
						},
						{
							StartTimestamp: ts1,
							LabelValues: []*metricspb.LabelValue{
								{Value: "400", HasValue: true},
								{Value: "post", HasValue: true},
							},
							Points: []*metricspb.Point{
								{Timestamp: ts2, Value: &metricspb.Point_DoubleValue{DoubleValue: 10.0}},
							},
						},
						{
							StartTimestamp: ts1,
							LabelValues: []*metricspb.LabelValue{
								{Value: "500", HasValue: true},
								{Value: "post", HasValue: true},
							},
							Points: []*metricspb.Point{
								{Timestamp: ts2, Value: &metricspb.Point_DoubleValue{DoubleValue: 3.0}},
							},
						},
					},
				},
			},
		}

		doCompare(t, want2, &m2)
	})

}

func doCompare(t *testing.T, want, got interface{}) {
	if !reflect.DeepEqual(got, want) {
		ww := string(exportertest.ToJSON(want))
		gg := string(exportertest.ToJSON(got))
		diff := cmp.Diff(ww, gg)
		t.Errorf("metricBuilder.Build() mismatch (-want +got):\n%v\n want=%v \n got=%v\n", diff, ww, gg)
	}
}

func toUnixNano(ts *timestamp.Timestamp) int64 {
	return ts.Seconds*1000000000 + int64(ts.Nanos)
}

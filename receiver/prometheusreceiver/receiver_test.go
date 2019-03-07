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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/textparse"
	"github.com/prometheus/prometheus/scrape"

	agentmetricspb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/metrics/v1"
	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
)

func TestReceiver(t *testing.T) {
	// Start a Prometheus server with various configurations
	nreqs := int64(0)
	done := make(chan bool)
	cst := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		select {
		case <-done:
			http.Error(rw, "Shutdown already", http.StatusBadRequest)
		default:
		}

		// The number of requests
		ireqPlus1 := atomic.AddInt64(&nreqs, 1)
		if ireqPlus1 > int64(len(pages)) {
			close(done)
			http.Error(rw, "No more pages", http.StatusBadRequest)
			return
		}

		page := pages[ireqPlus1-1]
		rw.Write([]byte(page))
	}))
	defer cst.Close()

	ux, _ := url.Parse(cst.URL)
	rawConfig := fmt.Sprintf(`
scrape_configs:
  - job_name: 'test'

    scrape_interval: 80ms

    static_configs:
      - targets: ['%s']`, ux.Host)

	ma := new(metricsAppender)
	cfg, err := config.Load(rawConfig)
	if err != nil {
		t.Fatalf("Failed to load the Prometheus configuration: %v", err)
	}

	recv, _ := receiverFromConfig(context.Background(), ma, cfg)
	defer recv.Cancel()

	// Wait until all the requests are sent.
	<-done

	recv.Flush()

	ma.forEachExportMetricsServiceRequest(func(ereq *agentmetricspb.ExportMetricsServiceRequest) {
		blob, _ := json.MarshalIndent(ereq, "", "  ")
		t.Logf("\n%s\n\n", blob)
	})
}

func TestProcessNonHistogramLikeMetrics(t *testing.T) {
	ma := new(metricsAppender)
	ca := newCustomAppender(ma)

	values := []struct {
		metricName string
		metadata   *scrape.MetricMetadata
		scheme     string
		labelsList labels.Labels
		wantErr    string
		timeAtMs   int64
		ref        uint64
		value      float64
		want       []*agentmetricspb.ExportMetricsServiceRequest
	}{
		{
			metricName: "call_counts",
			labelsList: labels.Labels{
				{Name: "client", Value: "mt-java@0.2.7"},
				{Name: "os", Value: "windows"},
				{Name: "method", Value: "java.sql.Statement.executeQuery"},
				{Name: "status", Value: "OK"},
			},
			value:    100.9,
			timeAtMs: 1549154046078,
			metadata: &scrape.MetricMetadata{
				Unit:   "1",
				Help:   "The number of calls",
				Metric: "call_counts",
				Type:   textparse.MetricType("counter"),
			},
			want: []*agentmetricspb.ExportMetricsServiceRequest{
				{
					Metrics: []*metricspb.Metric{
						{

							Descriptor_: &metricspb.Metric_MetricDescriptor{
								MetricDescriptor: &metricspb.MetricDescriptor{
									Name:        "call_counts",
									Description: "The number of calls",
									Unit:        "1",
									Type:        metricspb.MetricDescriptor_CUMULATIVE_INT64,
									LabelKeys: []*metricspb.LabelKey{
										{Key: "client"},
										{Key: "os"},
										{Key: "method"},
										{Key: "status"},
									},
								},
							},
							Timeseries: []*metricspb.TimeSeries{
								{
									StartTimestamp: timestampFromMs(1549154046078),
									LabelValues: []*metricspb.LabelValue{
										{Value: "mt-java@0.2.7"},
										{Value: "windows"},
										{Value: "java.sql.Statement.executeQuery"},
										{Value: "OK"},
									},
									Points: []*metricspb.Point{
										{
											Timestamp: timestampFromMs(1549154046078),
											Value: &metricspb.Point_Int64Value{
												Int64Value: 101,
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

	for i, tt := range values {
		err := ca.processNonHistogramLikeMetrics(tt.metricName, tt.metadata, tt.scheme, tt.labelsList, tt.ref, tt.timeAtMs, tt.value)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("#%d: unexpected error %v wanted %s", i, err, tt.wantErr)
			}
			continue
		}

		if err != nil {
			t.Errorf("#%d: unexpected error: %v", i, err)
			continue
		}
		ca.flush()

		got := ma.clearAllExportMetricsServiceRequest()
		if !reflect.DeepEqual(got, tt.want) {
			gb, wb := asJSON(got), asJSON(tt.want)
			if gb != wb {
				t.Errorf("#%d:\nGot:\n%s\n\nWant:\n%s\n\n", i, gb, wb)
			}
		}
	}
}

func asJSON(v interface{}) string {
	blob, _ := json.MarshalIndent(v, "", "  ")
	return string(blob)
}

type metricsAppender struct {
	mu    sync.Mutex
	ereqs []*agentmetricspb.ExportMetricsServiceRequest
}

var _ metricsSink = (*metricsAppender)(nil)

func (ma *metricsAppender) ReceiveMetrics(ctx context.Context, ereq *agentmetricspb.ExportMetricsServiceRequest) error {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	ma.ereqs = append(ma.ereqs, ereq)

	return nil
}

func (ma *metricsAppender) clearAllExportMetricsServiceRequest() []*agentmetricspb.ExportMetricsServiceRequest {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	ereqs := ma.ereqs[:]
	ma.ereqs = ma.ereqs[:0]

	return ereqs
}

func (ma *metricsAppender) forEachExportMetricsServiceRequest(fn func(ereq *agentmetricspb.ExportMetricsServiceRequest)) {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	for _, ereq := range ma.ereqs {
		fn(ereq)
	}
}

var pages = []string{
	`
# HELP opdemo_latency The various latencies of the methods
# TYPE opdemo_latency histogram
opdemo_latency_bucket{client="cli",method="repl",le="0"} 0
opdemo_latency_bucket{client="cli",method="repl",le="10"} 56
opdemo_latency_bucket{client="cli",method="repl",le="50"} 272
opdemo_latency_bucket{client="cli",method="repl",le="100"} 482
opdemo_latency_bucket{client="cli",method="repl",le="200"} 497
opdemo_latency_bucket{client="cli",method="repl",le="400"} 535
opdemo_latency_bucket{client="cli",method="repl",le="800"} 588
opdemo_latency_bucket{client="cli",method="repl",le="1000"} 609
opdemo_latency_bucket{client="cli",method="repl",le="1400"} 627
opdemo_latency_bucket{client="cli",method="repl",le="2000"} 630
opdemo_latency_bucket{client="cli",method="repl",le="5000"} 653
opdemo_latency_bucket{client="cli",method="repl",le="10000"} 680
opdemo_latency_bucket{client="cli",method="repl",le="15000"} 695
opdemo_latency_bucket{client="cli",method="repl",le="+Inf"} 702
opdemo_latency_sum{client="cli",method="repl"} 669824.6063739995
opdemo_latency_count{client="cli",method="repl"} 702
# HELP opdemo_process_counts The various counts
# TYPE opdemo_process_counts counter
opdemo_process_counts{client="cli",method="repl"} 702`,
	`
# HELP java_sql_client_latency The distribution of the latencies of various calls in milliseconds
# TYPE java_sql_client_latency histogram
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.1",} 13.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.5",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.5",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.5",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="25.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="50.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="400.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="600.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="800.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1500.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2500.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="20000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="40000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="500000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="+Inf",} 15.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 15.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 1.413127
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.1",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.5",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.0",} 14.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.5",} 14.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.0",} 14.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.5",} 14.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5.0",} 14.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10.0",} 14.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="25.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="50.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="400.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="600.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="800.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1500.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2500.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="20000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="40000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="500000.0",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="+Inf",} 15.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 15.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 19.856447999999997
# HELP java_sql_client_calls The number of various calls of methods
# TYPE java_sql_client_calls counter
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 15.0
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 15.0`,
	`
# HELP java_sql_client_latency The distribution of the latencies of various calls in milliseconds
# TYPE java_sql_client_latency histogram
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.1",} 15.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.5",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.5",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.5",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="25.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="50.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="400.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="600.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="800.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1500.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2500.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="20000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="40000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="500000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="+Inf",} 17.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 17.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 1.559018
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.1",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.5",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.0",} 16.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.5",} 16.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.0",} 16.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.5",} 16.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5.0",} 16.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10.0",} 16.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="25.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="50.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="400.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="600.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="800.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1500.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2500.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="20000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="40000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="500000.0",} 17.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="+Inf",} 17.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 17.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 21.197789999999998
# HELP java_sql_client_calls The number of various calls of methods
# TYPE java_sql_client_calls counter
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 17.0
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 17.0`,
	`
# HELP java_sql_client_latency The distribution of the latencies of various calls in milliseconds
# TYPE java_sql_client_latency histogram
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.05",} 1.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.1",} 18.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.5",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.5",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.5",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="25.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="50.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="400.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="600.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="800.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1500.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2500.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="20000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="40000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="500000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="+Inf",} 20.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 20.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 1.72292
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.1",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.5",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.0",} 19.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.5",} 19.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.0",} 19.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.5",} 19.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5.0",} 19.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10.0",} 19.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="25.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="50.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="400.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="600.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="800.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1500.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2500.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="20000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="40000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="500000.0",} 20.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="+Inf",} 20.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 20.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 22.761886999999994
# HELP java_sql_client_calls The number of various calls of methods
# TYPE java_sql_client_calls counter
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 20.0
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 20.0`,
	`
# HELP java_sql_client_latency The distribution of the latencies of various calls in milliseconds
# TYPE java_sql_client_latency histogram
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.05",} 1.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.1",} 19.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.5",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.5",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.5",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="25.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="50.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="400.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="600.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="800.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1500.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2500.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="20000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="40000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="500000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="+Inf",} 22.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 22.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 2.020241
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.1",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.5",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.0",} 21.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.5",} 21.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.0",} 21.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.5",} 21.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5.0",} 21.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10.0",} 21.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="25.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="50.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="400.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="600.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="800.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1500.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2500.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="20000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="40000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="500000.0",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="+Inf",} 22.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 22.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 23.859748999999994
# HELP java_sql_client_calls The number of various calls of methods
# TYPE java_sql_client_calls counter
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 22.0
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 22.0`,
	`
# HELP java_sql_client_latency The distribution of the latencies of various calls in milliseconds
# TYPE java_sql_client_latency histogram
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.05",} 1.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.1",} 22.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.5",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.5",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.5",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="25.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="50.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="400.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="600.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="800.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1500.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2500.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="20000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="40000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="500000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="+Inf",} 25.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 25.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 2.2258959999999997
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.1",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.5",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.0",} 24.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.5",} 24.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.0",} 24.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.5",} 24.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5.0",} 24.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10.0",} 24.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="25.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="50.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="400.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="600.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="800.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1500.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2500.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="20000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="40000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="500000.0",} 25.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="+Inf",} 25.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 25.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 25.51304399999999
# HELP java_sql_client_calls The number of various calls of methods
# TYPE java_sql_client_calls counter
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 25.0
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 25.0`,
	`
# HELP java_sql_client_latency The distribution of the latencies of various calls in milliseconds
# TYPE java_sql_client_latency histogram
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.05",} 2.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.1",} 24.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.5",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.5",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.5",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="25.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="50.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="400.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="600.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="800.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1500.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2500.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="20000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="40000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="500000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="+Inf",} 27.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 27.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 2.354682
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.1",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.5",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.0",} 26.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.5",} 26.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.0",} 26.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.5",} 26.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5.0",} 26.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10.0",} 26.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="25.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="50.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="400.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="600.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="800.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1500.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2500.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="20000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="40000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="500000.0",} 27.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="+Inf",} 27.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 27.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 26.768423999999992
# HELP java_sql_client_calls The number of various calls of methods
# TYPE java_sql_client_calls counter
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 27.0
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 27.0`,
	`
# HELP java_sql_client_latency The distribution of the latencies of various calls in milliseconds
# TYPE java_sql_client_latency histogram
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.05",} 2.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.1",} 26.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.5",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.5",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.5",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="25.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="50.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="400.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="600.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="800.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1500.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2500.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="20000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="40000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="500000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="+Inf",} 30.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 30.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 2.589916
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.1",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.5",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.0",} 29.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.5",} 29.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.0",} 29.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.5",} 29.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5.0",} 29.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10.0",} 29.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="25.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="50.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="400.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="600.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="800.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1500.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2500.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="20000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="40000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="500000.0",} 30.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="+Inf",} 30.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 30.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 28.888423999999993
# HELP java_sql_client_calls The number of various calls of methods
# TYPE java_sql_client_calls counter
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 30.0
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 30.0`,
	`
# HELP java_sql_client_latency The distribution of the latencies of various calls in milliseconds
# TYPE java_sql_client_latency histogram
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.05",} 2.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.1",} 28.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.5",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.5",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.5",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="25.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="50.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="400.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="600.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="800.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1500.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2500.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="20000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="40000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="500000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="+Inf",} 32.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 32.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 2.7239299999999997
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.1",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.5",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.0",} 31.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.5",} 31.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.0",} 31.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.5",} 31.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5.0",} 31.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10.0",} 31.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="25.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="50.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="400.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="600.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="800.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1500.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2500.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="20000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="40000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="500000.0",} 32.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="+Inf",} 32.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 32.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 30.145545999999992
# HELP java_sql_client_calls The number of various calls of methods
# TYPE java_sql_client_calls counter
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 32.0
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 32.0`,
	`
# HELP java_sql_client_latency The distribution of the latencies of various calls in milliseconds
# TYPE java_sql_client_latency histogram
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.05",} 3.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.1",} 31.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.5",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.5",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.5",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="25.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="50.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="400.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="600.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="800.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1500.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2500.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="20000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="40000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="500000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="+Inf",} 35.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 35.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 2.927028999999999
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.1",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.5",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.0",} 34.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.5",} 34.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.0",} 34.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.5",} 34.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5.0",} 34.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10.0",} 34.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="25.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="50.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="400.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="600.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="800.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1500.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2500.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="20000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="40000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="500000.0",} 35.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="+Inf",} 35.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 35.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 32.041439
# HELP java_sql_client_calls The number of various calls of methods
# TYPE java_sql_client_calls counter
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 35.0
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 35.0`,
	`
# HELP java_sql_client_latency The distribution of the latencies of various calls in milliseconds
# TYPE java_sql_client_latency histogram
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.05",} 3.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.1",} 33.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.5",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.5",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.5",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="25.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="50.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="400.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="600.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="800.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1500.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2500.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="20000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="40000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="500000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="+Inf",} 37.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 37.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 3.059655
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.1",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.5",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.0",} 36.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.5",} 36.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.0",} 36.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.5",} 36.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5.0",} 36.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10.0",} 36.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="25.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="50.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="400.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="600.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="800.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1500.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2500.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="20000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="40000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="500000.0",} 37.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="+Inf",} 37.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 37.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 33.182669999999995
# HELP java_sql_client_calls The number of various calls of methods
# TYPE java_sql_client_calls counter
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 37.0
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 37.0`,
	`
# HELP java_sql_client_latency The distribution of the latencies of various calls in milliseconds
# TYPE java_sql_client_latency histogram
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.05",} 3.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.1",} 36.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.5",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.5",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.5",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="25.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="50.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="400.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="600.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="800.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1500.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2500.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="20000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="40000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="500000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="+Inf",} 40.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 40.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 3.2418199999999993
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.1",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.5",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.0",} 39.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.5",} 39.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.0",} 39.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.5",} 39.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5.0",} 39.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10.0",} 39.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="25.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="50.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="400.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="600.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="800.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1500.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2500.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="20000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="40000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="500000.0",} 40.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="+Inf",} 40.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 40.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 35.05305899999999
# HELP java_sql_client_calls The number of various calls of methods
# TYPE java_sql_client_calls counter
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 40.0
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 40.0`,
	`
# HELP java_sql_client_latency The distribution of the latencies of various calls in milliseconds
# TYPE java_sql_client_latency histogram
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.05",} 4.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.1",} 38.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.5",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.5",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.5",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="25.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="50.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="400.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="600.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="800.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1500.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2500.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="20000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="40000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="500000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="+Inf",} 42.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 42.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 3.345085999999999
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.1",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.5",} 1.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.0",} 41.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.5",} 41.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.0",} 41.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.5",} 41.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5.0",} 41.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10.0",} 41.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="25.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="50.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="400.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="600.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="800.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1500.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2500.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="20000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="40000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="500000.0",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="+Inf",} 42.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 42.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 36.209157999999995
# HELP java_sql_client_calls The number of various calls of methods
# TYPE java_sql_client_calls counter
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 42.0
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 42.0`,
	`
# HELP java_sql_client_latency The distribution of the latencies of various calls in milliseconds
# TYPE java_sql_client_latency histogram
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.05",} 5.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.1",} 41.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.5",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.5",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.5",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="25.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="50.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="400.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="600.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="800.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1500.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2500.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="20000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="40000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="500000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="+Inf",} 45.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 45.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 3.5096209999999988
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.1",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.5",} 1.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.0",} 44.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.5",} 44.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.0",} 44.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.5",} 44.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5.0",} 44.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10.0",} 44.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="25.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="50.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="400.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="600.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="800.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1500.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2500.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="20000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="40000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="500000.0",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="+Inf",} 45.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 45.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 38.07137699999999
# HELP java_sql_client_calls The number of various calls of methods
# TYPE java_sql_client_calls counter
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 45.0
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 45.0`,
	`
# HELP java_sql_client_latency The distribution of the latencies of various calls in milliseconds
# TYPE java_sql_client_latency histogram
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.05",} 5.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.1",} 42.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.5",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.5",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.5",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="25.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="50.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="400.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="600.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="800.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1500.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2500.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="20000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="40000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="500000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="+Inf",} 47.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 47.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 3.798140999999999
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.1",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.5",} 1.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.0",} 46.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.5",} 46.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.0",} 46.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.5",} 46.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5.0",} 46.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10.0",} 46.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="25.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="50.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="400.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="600.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="800.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1500.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2500.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="20000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="40000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="500000.0",} 47.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="+Inf",} 47.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 47.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 39.12458199999999
# HELP java_sql_client_calls The number of various calls of methods
# TYPE java_sql_client_calls counter
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 47.0
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 47.0`,
	`
# HELP java_sql_client_latency The distribution of the latencies of various calls in milliseconds
# TYPE java_sql_client_latency histogram
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.05",} 6.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.1",} 45.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="0.5",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1.5",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2.5",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="25.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="50.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="400.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="600.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="800.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="1500.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="2500.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="5000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="10000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="20000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="40000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="100000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="200000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="500000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",le="+Inf",} 50.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 50.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 3.9551329999999987
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.0",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.001",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.005",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.01",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.05",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.1",} 0.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="0.5",} 1.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.0",} 49.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1.5",} 49.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.0",} 49.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2.5",} 49.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5.0",} 49.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10.0",} 49.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="25.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="50.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="400.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="600.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="800.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="1500.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="2500.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="5000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="10000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="20000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="40000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="100000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="200000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="500000.0",} 50.0
java_sql_client_latency_bucket{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",le="+Inf",} 50.0
java_sql_client_latency_count{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 50.0
java_sql_client_latency_sum{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 40.71351599999999
# HELP java_sql_client_calls The number of various calls of methods
# TYPE java_sql_client_calls counter
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.ResultSet.close",java_sql_status="OK",} 50.0
java_sql_client_calls{java_sql_error="",java_sql_method="java.sql.Statement.executeQuery",java_sql_status="OK",} 50.0`,
}

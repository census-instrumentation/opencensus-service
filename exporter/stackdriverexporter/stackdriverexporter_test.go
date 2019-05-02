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

package stackdriverexporter

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestStackdriverTraceExportersFromViper(t *testing.T) {
	// we set location in our test configs to avoid trying to
	// contact metadata service.
	testCases := []struct {
		name       string
		config     map[string]interface{}
		wantErr    string
		traceLen   int
		metricsLen int
	}{
		{
			name: "empty config",
		},
		{
			name: "tracing enabled",
			config: map[string]interface{}{
				"project":        "no-such-project",
				"enable_tracing": true,
				"location":       "local-test",
			},
			traceLen: 1,
		},
		{
			name: "metrics enabled",
			config: map[string]interface{}{
				"project":        "no-such-project",
				"enable_metrics": true,
				"location":       "local-test",
			},
			metricsLen: 1,
		},
		{
			name: "metrics and trace enabled",
			config: map[string]interface{}{
				"project":        "no-such-project",
				"enable_tracing": true,
				"enable_metrics": true,
				"location":       "local-test",
			},
			metricsLen: 1,
			traceLen:   1,
		},
		{
			name: "trace attributes",
			config: map[string]interface{}{
				"project":        "no-such-project",
				"enable_tracing": true,
				"location":       "local-test",
				"trace_attributes": map[string]interface{}{
					"key": "value",
				},
			},
			traceLen: 1,
		},
		{
			name: "metrics labels",
			config: map[string]interface{}{
				"project":        "no-such-project",
				"enable_metrics": true,
				"location":       "local-test",
				"metric_labels": map[string]string{
					"key": "value",
				},
			},
			metricsLen: 1,
		},
	}

	for _, test := range testCases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			v := viper.New()
			v.Set("stackdriver", test.config)

			tps, mps, _, err := StackdriverTraceExportersFromViper(v)

			if test.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error, but did not get one")
				}
				if !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("wanted %q in error but got %v", test.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}

			if len(tps) != test.traceLen {
				t.Fatalf("expected %d trace exporters, but got %d", test.traceLen, len(tps))
			}

			if len(mps) != test.metricsLen {
				t.Fatalf("expected %d trace exporters, but got %d", test.metricsLen, len(mps))
			}
		})
	}

}

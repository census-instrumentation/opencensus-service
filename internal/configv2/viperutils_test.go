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

package configv2

import (
	"testing"
)

func TestValidGetViperSubSequenceOfMaps(t *testing.T) {
	var yaml = `
receivers:
  - type: opencensus
    name: myreceiver
    address: "127.0.0.1"
    port: 12345
    enabled: true
  - type: opencensus2
    protocols:
    - protocol: proto1
      address: "127.0.0.1"
      port: 2345
      enabled: false
    - protocol: proto2
      address: "localhost"
      port: 3456
      enabled: true
`

	v, err := readConfigFromYamlStr(yaml)
	if err != nil {
		t.Fatalf("Unable to read yaml: %v", err)
	}

	subs, err := getViperSubSequenceOfMaps(v, "receivers")
	if err != nil {
		t.Fatalf("Unable to decode yaml: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("Expected 2 receivers sections")
	}

	protocols, err := getViperSubSequenceOfMaps(subs[1], "protocols")
	if err != nil {
		t.Fatalf("Unable to decode yaml: %v", err)
	}
	if len(protocols) != 2 {
		t.Fatalf("Unable to decode yaml")
	}
}

func TestInvalidGetViperSubSequenceOfMaps(t *testing.T) {
	var testCases = []string{
		`
receivers:
  - scalar
`,
		`
receivers:
  key: value
`,
	}

	for _, testCase := range testCases {
		v, err := readConfigFromYamlStr(testCase)
		if err != nil {
			t.Fatalf("Unable to read yaml: %v", err)
		}

		_, err = getViperSubSequenceOfMaps(v, "receivers")
		if err == nil {
			t.Fatalf("Expected to fail decoding invalid subsequence of maps")
		}
	}
}

func TestInvalidReadConfigFromYamlStr(t *testing.T) {
	var yaml = `
receivers:
  - type: jaeger
    - protocol: thrift-tchannel
      enabled: true
    - protocol: thrift-http
      enabled: true
`

	_, err := readConfigFromYamlStr(yaml)
	if err == nil {
		t.Fatalf("Must fail on invalid yaml")
	}
}

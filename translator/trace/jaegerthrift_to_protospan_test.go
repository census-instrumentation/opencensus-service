// Copyright 2018, OpenCensus Authors
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

package tracetranslator

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/census-instrumentation/opencensus-service/internal/testutils"

	"github.com/jaegertracing/jaeger/thrift-gen/jaeger"
)

func TestJaegerThriftBatchToOCProto(t *testing.T) {
	const numOfFiles = 2
	for i := 1; i <= 2; i++ {
		thriftInFile := fmt.Sprintf("./testdata/thrift_batch_%02d.json", i)
		jb := loadJaegerThriftBatchFromJSON(t, thriftInFile)
		octrace, err := JaegerThriftBatchToOCProto(jb)
		if err != nil {
			t.Errorf("Failed to handled Jaeger Thrift Batch from %q. Error: %v", thriftInFile, err)
		}

		wantedSpanCount, receivedSpanCount := len(jb.Spans), len(octrace.Spans)
		if wantedSpanCount != receivedSpanCount {
			t.Errorf("Different number of spans in the batches on pass #%d (expected %d, got %d)", i, wantedSpanCount, receivedSpanCount)
		}

		gb, err := json.MarshalIndent(octrace, "", "  ")
		if err != nil {
			t.Errorf("Failed to convert received OC proto to json. Error: %v", err)
		}

		protoFile := fmt.Sprintf("./testdata/ocproto_batch_%02d.json", i)
		wb, err := ioutil.ReadAll(getFile(t, protoFile))
		if err != nil {
			t.Errorf("Failed to read file %q with expected OC proto in JSON format. Error: %v", protoFile, err)
		}

		gj, wj := testutils.GenerateNormalizedJSON(string(gb)), testutils.GenerateNormalizedJSON(string(wb))
		if gj != wj {
			t.Errorf("The roundtrip JSON doesn't match the JSON that we want\nGot:\n%s\nWant:\n%s", gj, wj)
		}
	}
}

func loadJaegerThriftBatchFromJSON(t *testing.T, file string) *jaeger.Batch {
	r := &jaeger.Batch{}
	loadFromJSON(t, file, &r)
	return r
}

func loadFromJSON(t *testing.T, file string, obj interface{}) {
	jsonReader := getFile(t, file)
	err := json.NewDecoder(jsonReader).Decode(obj)
	if err != nil {
		t.Errorf("Failed to decode json file %q into type %T", file, obj)
	}
}

func getFile(t *testing.T, file string) *os.File {
	r, err := os.Open(file)
	if err != nil {
		t.Errorf("Failed to open file %q", file)
	}
	return r
}

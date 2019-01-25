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

package tracetranslator

import (
	"encoding/json"
	"io/ioutil"
	"reflect"
	"testing"

	"github.com/jaegertracing/jaeger/thrift-gen/zipkincore"
)

func TestZipkinV1ThriftToOCProto(t *testing.T) {
	blob, err := ioutil.ReadFile("./testdata/zipkin_v1_thrift_single_batch.json")
	if err != nil {
		t.Fatalf("failed to load test data: %v", err)
	}

	var ztSpans []*zipkincore.Span
	err = json.Unmarshal(blob, &ztSpans)
	if err != nil {
		t.Fatalf("failed to unmarshal json into zipkin v1 thrift: %v", err)
	}

	got, err := ZipkinV1ThriftBatchToOCProto(ztSpans)
	if err != nil {
		t.Fatalf("failed to translate zipkinv1 thrift to OC proto: %v", err)
	}

	want := ocBatchesFromZipkinV1
	sortTraceByNodeName(want)
	sortTraceByNodeName(got)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got different data than want")
	}
}

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
	"os"
	"sort"
	"testing"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	jaeger "github.com/jaegertracing/jaeger/model"

	"github.com/census-instrumentation/opencensus-service/internal/testutils"
)

func loadPBFromJSON(file string, msg proto.Message) error {
	f, err := os.Open(file)
	if err == nil {
		err = jsonpb.Unmarshal(f, msg)
	}
	defer f.Close()
	return err
}

func TestOCProtoToJaegerProto(t *testing.T) {
	const numOfFiles = 2
	for i := 1; i < numOfFiles; i++ {
		ocBatch := ocBatches[i]

		gotJBatch, err := OCProtoToJaegerProto(ocBatch)
		if err != nil {
			t.Errorf("Failed to translate OC batch to Jaeger Proto: %v", err)
			continue
		}

		wantSpanCount, gotSpanCount := len(ocBatch.Spans), len(gotJBatch.Spans)
		if wantSpanCount != gotSpanCount {
			t.Errorf("Different number of spans in the batches on pass #%d (want %d, got %d)", i, wantSpanCount, gotSpanCount)
			continue
		}

		// Jaeger binary tags do not round trip from Jaeger -> OCProto -> Jaeger.
		// For tests use data without binary tags.
		protoFile := fmt.Sprintf("./testdata/jaegerproto_batch_%02d.json", i+1)
		wantJBatch := &jaeger.Batch{}
		if err := loadFromJSON(protoFile, wantJBatch); err != nil {
			t.Errorf("Failed load Jaeger Proto from %q: %v", protoFile, err)
			continue
		}

		// Sort tags to help with comparison, not only for jaeger.Process but also
		// on each span.
		sort.Slice(gotJBatch.Process.Tags, func(i, j int) bool {
			return gotJBatch.Process.Tags[i].Key < gotJBatch.Process.Tags[j].Key
		})
		sort.Slice(wantJBatch.Process.Tags, func(i, j int) bool {
			return wantJBatch.Process.Tags[i].Key < wantJBatch.Process.Tags[j].Key
		})
		var jSpans []*jaeger.Span
		jSpans = append(jSpans, gotJBatch.Spans...)
		jSpans = append(jSpans, wantJBatch.Spans...)
		for _, jSpan := range jSpans {
			sort.Slice(jSpan.Tags, func(i, j int) bool {
				return jSpan.Tags[i].Key < jSpan.Tags[j].Key
			})
			if jSpan.Process == nil {
				continue
			}
			sort.Slice(jSpan.Process.Tags, func(i, j int) bool {
				return jSpan.Process.Tags[i].Key < jSpan.Process.Tags[j].Key
			})
		}

		gjson, _ := json.Marshal(gotJBatch)
		wjson, _ := json.Marshal(wantJBatch)
		gjsonStr := testutils.GenerateNormalizedJSON(string(gjson))
		wjsonStr := testutils.GenerateNormalizedJSON(string(wjson))
		if gjsonStr != wjsonStr {
			t.Errorf("OC Proto to Jaeger Proto failed.\nGot:\n%s\nWant:\n%s\n", gjsonStr, wjsonStr)
		}
	}
}

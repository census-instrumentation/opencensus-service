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

// Program occollector receives stats and traces from multiple sources and
// batches them for appropriate forwarding to backends (e.g.: Jaeger or Zipkin)
// or other layers of occollector. The forwarding can be configured so
// buffer sizes, number of retries, backoff policy, etc can be ajusted according
// to specific needs of each deployment.
package main

import (
	"log"

	"github.com/census-instrumentation/opencensus-service/cmd/occollector/app/collector"
)

func main() {
	if err := collector.App.Start(); err != nil {
		log.Fatalf("Failed to run the collector: %v", err)
	}
}

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

package storage_test

import (
	"testing"
	"time"

	"github.com/census-instrumentation/opencensus-agent/internal/storage"
)

func TestInMemoryKeyValueStore(t *testing.T) {
	ikvs := storage.NewInMemoryKeyValueStore()

	// This test ensures that we never race i.e. that
	// updates and reads should always be synchronized.
	key := "ikvs-test"
	value := struct{ Name string }{Name: "This test"}

	// For the first write
	ikvs.Put(key, value)

	// Ensure that writes interleave with reads by setting their periods
	// as multiples of each other but setting a large enough n so
	// that there will be optimistically (n-1) concurrent coincidences.
	n := 40
	period := 5 * time.Nanosecond
	readTimer := time.NewTicker(period)
	writeTimer := time.NewTicker(period)
	defer func() {
		readTimer.Stop()
		writeTimer.Stop()
	}()

	readsDoneChan := make(chan bool)

	go func() {
		defer func() {
			close(readsDoneChan)
		}()

		for i := 0; i < n; i++ {
			got, ok, err := ikvs.Get(key)
			if err != nil || !ok || got != value {
				t.Errorf("Iteration #%d. Mismatch:: Got (%+v) Want (%+v) Error %v", i, got, value, err)
			}
			<-readTimer.C
		}
	}()

	for i := 0; i < n; i++ {
		ikvs.Put(key, value)
		<-writeTimer.C
	}

	<-readsDoneChan
}

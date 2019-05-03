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

// Package collector handles the command-line, configuration, and runs the OC collector.
package collector

import (
	"net/http"
	"testing"
)

func TestApplication_Start(t *testing.T) {
	// Without exporters the collector will start and just shutdown, no error is expected.
	App.v.Set("logging-exporter", true)
	appDone := make(chan struct{})
	go func() {
		if err := App.Start(); err != nil {
			t.Fatalf("App.Start() got %v, want nil", err)
		}
		close(appDone)
	}()

	<-App.readyChan
	if !isAppReady(t) {
		t.Fatalf("App didn't reach ready state")
	}
	close(App.stopTestChan)
	<-appDone
}

func isAppReady(t *testing.T) bool {
	client := &http.Client{}
	resp, err := client.Get("http://localhost:13133")
	if err != nil {
		t.Fatalf("failed to get a response from health probe: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusNoContent
}

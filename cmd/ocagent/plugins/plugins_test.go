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

/*
This test serves as an end-to-end canonical example of consuming a TraceExporter
plugin to ensure that the code for parsing out plugins never regresses.
*/
package plugins_test

import (
	"bytes"
	"context"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"github.com/census-instrumentation/opencensus-service/cmd/ocagent/plugins"
)

func TestCreatePlugin(t *testing.T) {
	t.Skip("Skipping for now as making Go versions match on Travis CI is a hassle")

	// Compile the plugin and make a .so
	tmpDir, err := ioutil.TempDir("", "plugin_end_to_end")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	goBinaryPath := properGoBinaryPath()
	pluginPath := filepath.Join(tmpDir, "sample.so")
	cmd := exec.Command(goBinaryPath, "build", "-buildmode=plugin", "-o", pluginPath, "./testdata/sample_plugin.go")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to compile and create shared object file %q: %v\n%s\nGoBinPath: %q", pluginPath, err, output, goBinaryPath)
	}

	configYAML := `
counter:
    count: 1
        `

	// Given that plugins.LoadTraceExporterPlugins writes errors to log.Output
	// we'll hijack that writers and ensure that we get NO output
	checkWriter := new(bytes.Buffer)
	log.SetOutput(checkWriter)
	// Before we exit this test, revert the log output writer to stderr.
	defer log.SetOutput(os.Stderr)

	// Now that we have the plugin written to disk, let's now load it.
	sr, stopFn := plugins.LoadTraceExporterPlugins([]byte(configYAML), pluginPath)
	if sr == nil {
		t.Fatal("Failed to create a spanreceiver")
	}
	defer stopFn()

	node := &commonpb.Node{}
	span := &tracepb.Span{
		TraceId: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
	}
	sr.ReceiveSpans(context.Background(), node, span)
	<-time.After(5 * time.Millisecond)

	// Now check if we got any output
	if g := checkWriter.String(); g != "" {
		t.Errorf("Unexpected output: %s", g)
	}
}

// This helper function is necessary to ensure that we use
// the same Go binary to compile plugins as well as to run
// this test.
// Otherwise we'll run into such failed tests:
//      https://travis-ci.org/census-instrumentation/opencensus-service/builds/438157975
func properGoBinaryPath() string {
	goSuffix := "go"
	if runtime.GOOS == "windows" {
		goSuffix += ".exe"
	}
	// Firstly check if we are using $GOROOT/bin/go*
	goBinPath := filepath.Join(runtime.GOROOT(), "bin", goSuffix)
	if _, err := os.Stat(goBinPath); err == nil {
		return goBinPath
	}
	// If that has failed, now trying looking it from the environment
	binPath, _ := exec.LookPath(goSuffix)
	return binPath
}

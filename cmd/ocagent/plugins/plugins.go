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

package plugins

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"plugin"
	"strings"

	"github.com/census-instrumentation/opencensus-service/exporter"
	"github.com/census-instrumentation/opencensus-service/spanreceiver"
)

type pluginLoadStatus struct {
	absPluginPath  string
	pluginPath     string
	err            error
	traceExporters []exporter.TraceExporter
	stopFns        []func(context.Context) error
}

// loadPluginsFromYAML is a helper that takes in the YAML configuration file as well as the paths
// to the various plugins. It expects each path to be resolvable locally but that the loaded plugin
// contain the "constructor" symbol "NewTraceExporterPlugin" of type `func() exporter.TraceExporterPlugin`
func loadPluginsFromYAML(configYAML []byte, pluginPaths []string) (results []*pluginLoadStatus) {
	for _, pluginPath := range pluginPaths {
		absPluginPath, err := filepath.Abs(pluginPath)
		status := &pluginLoadStatus{
			pluginPath:    pluginPath,
			absPluginPath: absPluginPath,
		}
		if err != nil {
			status.err = err
			continue
		}
		// Firstly ensure we add the result regardless of parse errors.
		results = append(results, status)
		p, err := plugin.Open(absPluginPath)
		if err != nil {
			status.err = err
			continue
		}

		iface, err := p.Lookup("NewTraceExporterPlugin")
		if err != nil {
			status.err = err
			continue
		}
		constructor, ok := iface.(func() exporter.TraceExporterPlugin)
		if !ok {
			status.err = fmt.Errorf("Invalid Type for NewTraceExporterPlugin: %T", iface)
			continue
		}
		tplugin := constructor()
		status.stopFns = append(status.stopFns, tplugin.Stop)
		status.traceExporters, err = tplugin.TraceExportersFromConfig(configYAML, exporter.ConfigTypeYAML)
		if err != nil {
			status.err = fmt.Errorf("Failed to parse YAML: %v", err)
		}
	}
	return
}

// LoadTraceExporterPlugins handles loading TraceExporter plugins from the various
// paths. On failed loads, it logs the error and the failed plugin path.
func LoadTraceExporterPlugins(configYAML []byte, pluginPathsCSV string) (sr spanreceiver.SpanReceiver, stopFn func()) {
	var exporters []exporter.TraceExporter
	var stopFns []func(context.Context) error
	defer func() {
		stopFn = func() {
			ctx := context.Background()
			for _, stop := range stopFns {
				_ = stop(ctx)
			}
		}
	}()

	if pluginPathsCSV == "" {
		return
	}
	pluginPaths := strings.Split(pluginPathsCSV, ",")
	results := loadPluginsFromYAML(configYAML, pluginPaths)
	for _, result := range results {
		if result.err != nil {
			msg := "Failed to load the plugin from " + result.pluginPath
			if result.pluginPath != result.absPluginPath {
				msg += " (" + result.absPluginPath + ")"
			}
			log.Printf("%s Error: %v", msg, result.err)
			continue
		}
		exporters = append(exporters, result.traceExporters...)
		stopFns = append(stopFns, result.stopFns...)
	}

	sr = exporter.TraceExportersToSpanReceiver(exporters...)
	return
}

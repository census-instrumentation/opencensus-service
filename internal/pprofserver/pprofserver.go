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

package pprofserver

import (
	"flag"
	"net/http"
	_ "net/http/pprof" // Needed to enable the performance profiler
	"strconv"

	"go.uber.org/zap"
)

const (
	httpPprofPortCfg = "http-pprof-port"
)

// pport is used to ensure that if AddFlags was called the setup works even if viper
// was not set.
var pport *uint

// AddFlags add the command-line flags used to control the Performance Profiler
// (pprof) HTTP server to the given flag set.
func AddFlags(flags *flag.FlagSet) {
	pport = flags.Uint(
		httpPprofPortCfg,
		0,
		"Port to be used by golang net/http/pprof (Performance Profiler), the profiler is disabled if no port or 0 is specified.")
}

// SetupPerFlags sets up the Performance Profiler (pprof) as an HTTP endpoint
// according to the command-line flags.
func SetupPerFlags(asyncErrorChannel chan<- error, logger *zap.Logger) error {
	// TODO: (@pjanotti) when configuration switches to viper remove pport and use viper instead.
	if pport == nil && *pport == 0 {
		return nil
	}

	port := int(*pport)
	logger.Info("Starting net/http/pprof server", zap.Int("port", port))
	go func() {
		if err := http.ListenAndServe(":"+strconv.Itoa(port), nil); err != http.ErrServerClosed {
			asyncErrorChannel <- err
		}
	}()

	return nil
}

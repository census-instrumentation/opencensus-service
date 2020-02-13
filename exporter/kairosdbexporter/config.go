// Copyright 2020, OpenCensus Authors
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

package kairosdbexporter

import (
	"strings"

	"go.uber.org/zap/zapcore"
)

var levels = map[string]zapcore.Level{
	"debug": zapcore.DebugLevel,
	"info":  zapcore.InfoLevel,
	"warn":  zapcore.WarnLevel,
	"error": zapcore.ErrorLevel,
	"panic": zapcore.PanicLevel,
	"fatal": zapcore.FatalLevel,
	"_":     zapcore.InfoLevel,
}

type wrapperCfg struct {
	Config *config `mapstructure:"kairosdb"`
}

// httpCfg provides the configuration options for the underlining http clients used to communicate
// with the KairosDB REST API.
type httpCfg struct {
	MaxConnections           int  `mapstructure:"max_connections"`
	ConnectionTimeoutSeconds int  `mapstructure:"connection_timeout"`
	UseCompression           bool `mapstructure:"use_compression"`
}

// kairosDbConfig provides the configuration options for kairosdb TSDB API.
type config struct {
	// Endpoint specifies the KairosDB location.
	Endpoint string `mapstructure:"endpoint"`

	// LogLevel controls the level of logging for this exporter. Possible supported values are:
	// debug, info, warn, error, panic, fatal
	LogLevel string `mapstructure:"log_level"`

	// LogConsumedMetrics determines if we want to log all consumed metrics or not. This is always logged
	// using debug level.
	LogConsumedMetrics bool `mapstructure:"log_consumed_metrics"`

	// LogProducedMessages determines if we need to log the payloads we are shipping to kairosdb.
	LogProducedMessages bool `mapstructure:"log_produced_metrics"`

	// HTTPConfig stores the configuration for the http client.
	HTTPConfig *httpCfg `mapstructure:"http"`
}

// GetZapLevel returns the log level which must be used by the kairosdb exporter.
func (c *config) GetZapLevel() zapcore.Level {
	levelStr := strings.ToLower(c.LogLevel)
	if value, found := levels[levelStr]; found {
		return value
	}

	return levels["_"]
}

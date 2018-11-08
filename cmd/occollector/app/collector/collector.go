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

// Package collector handles the command-line, configuration, and runs the OC collector.
package collector

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/census-instrumentation/opencensus-service/internal/collector/jaeger"
	"github.com/census-instrumentation/opencensus-service/internal/collector/opencensus"
	"github.com/census-instrumentation/opencensus-service/internal/collector/processor"
	"github.com/census-instrumentation/opencensus-service/internal/collector/zipkin"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	configCfg             = "config"
	logLevelCfg           = "log-level"
	runJaegerCollectorCfg = "run-jaeger-collector"
	runOCReceiverCfg      = "run-oc-receiver"
	runZipkinReceiverCfg  = "run-zipkin-receiver"
)

var (
	config string

	v = viper.New()

	rootCmd = &cobra.Command{
		Use:  "occollector",
		Long: "OpenCensus Collector",
		Run: func(cmd *cobra.Command, args []string) {
			if file := v.GetString(configCfg); file != "" {
				v.SetConfigFile(file)
				err := v.ReadInConfig()
				if err != nil {
					log.Fatalf("Error loading config file %q. Error: %v", file, err)
					return
				}
			}

			execute()
		},
	}
)

func init() {
	rootCmd.PersistentFlags().String(logLevelCfg, "INFO", "Output level of logs (TRACE, DEBUG, INFO, WARN, ERROR, FATAL)")
	v.BindPFlag(logLevelCfg, rootCmd.PersistentFlags().Lookup(logLevelCfg))

	// local flags
	rootCmd.Flags().StringVar(&config, configCfg, "", "Path to the config file")
	rootCmd.Flags().Bool(runJaegerCollectorCfg, false, "Flag to run the jaeger-collector")
	rootCmd.Flags().Bool(runOCReceiverCfg, false, "Flag to run the OpenCensus receiver")
	rootCmd.Flags().Bool(runZipkinReceiverCfg, false, "Flag to run the Zipkin receiver")

	// TODO: (@pjanotti) add builder options as flags, before calls bellow. Likely it will require code re-org.

	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.BindPFlags(rootCmd.Flags())
}

func newLogger() (*zap.Logger, error) {
	var level zapcore.Level
	err := (&level).UnmarshalText([]byte(v.GetString(logLevelCfg)))
	if err != nil {
		return nil, err
	}
	conf := zap.NewProductionConfig()
	conf.Level.SetLevel(level)
	return conf.Build()
}

func execute() {
	var signalsChannel = make(chan os.Signal)
	signal.Notify(signalsChannel, os.Interrupt, syscall.SIGTERM)

	logger, err := newLogger()
	if err != nil {
		log.Fatalf("Failed to get logger: %v", err)
	}

	logger.Info("Starting...")

	// Build pipeline from its end: 1st exporters, the OC-proto queue processor, and
	// finally the receivers.

	// TODO: build exporters, plug them to processors.

	// TODO: construct multi-processor from config options, for now provisory code using a noop processor.
	spanProcessor := processor.NewNoopSpanProcessor(logger)

	var closeFns []func()
	var someReceiverEnabled bool
	receivers := []struct {
		name  string
		runFn func(*zap.Logger, *viper.Viper, processor.SpanProcessor) (func(), error)
		ok    bool
	}{
		{"Jaeger", jaegerreceiver.Run, v.GetBool(runJaegerCollectorCfg)},
		{"OpenCensus", ocreceiver.Run, v.GetBool(runOCReceiverCfg)},
		{"Zipkin", zipkinreceiver.Run, v.GetBool(runZipkinReceiverCfg)},
	}

	for _, receiver := range receivers {
		if receiver.ok {
			closeSrv, err := receiver.runFn(logger, v, spanProcessor)
			if err != nil {
				logger.Fatal("Cannot run receiver for "+receiver.name, zap.Error(err))
			}
			closeFns = append(closeFns, closeSrv)
			someReceiverEnabled = true
		}
	}

	if !someReceiverEnabled {
		logger.Warn("Nothing to do: no receiver was enabled. Shutting down.")
		os.Exit(1)
	}

	logger.Info("Collector is up and running.")

	<-signalsChannel
	logger.Info("Starting shutdown...")

	// TODO: orderly shutdown: first receivers, then flushing pipelines giving
	// senders a chance to send all their data. This may take time, the allowed
	// time should be part of configuration.
	for _, closeFn := range closeFns {
		closeFn()
	}

	logger.Info("Shutdown complete.")
}

// Execute the application according to the command and configuration given
// by the user.
func Execute() error {
	return rootCmd.Execute()
}

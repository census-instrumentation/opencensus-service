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
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	tchReporter "github.com/jaegertracing/jaeger/cmd/agent/app/reporter/tchannel"
	"github.com/jaegertracing/jaeger/pkg/healthcheck"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/uber/jaeger-lib/metrics"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/census-instrumentation/opencensus-service/cmd/occollector/app/builder"
	"github.com/census-instrumentation/opencensus-service/cmd/occollector/app/sender"
	"github.com/census-instrumentation/opencensus-service/exporter"
	"github.com/census-instrumentation/opencensus-service/exporter/exporterparser"
	"github.com/census-instrumentation/opencensus-service/internal"
	"github.com/census-instrumentation/opencensus-service/internal/collector/jaeger"
	"github.com/census-instrumentation/opencensus-service/internal/collector/opencensus"
	"github.com/census-instrumentation/opencensus-service/internal/collector/processor"
	"github.com/census-instrumentation/opencensus-service/internal/collector/telemetry"
	"github.com/census-instrumentation/opencensus-service/internal/collector/zipkin"
)

const (
	configCfg           = "config"
	healthCheckHTTPPort = "health-check-http-port"
	logLevelCfg         = "log-level"
	metricsLevelCfg     = "metrics-level"
	metricsPortCfg      = "metrics-port"
	jaegerReceiverFlg   = "receive-jaeger"
	ocReceiverFlg       = "receive-oc-trace"
	zipkinReceiverFlg   = "receive-zipkin"
	debugProcessorFlg   = "debug-processor"
)

var (
	config string

	v = viper.New()

	logger *zap.Logger

	rootCmd = &cobra.Command{
		Use:  "occollector",
		Long: "OpenCensus Collector",
		Run: func(cmd *cobra.Command, args []string) {
			if file := v.GetString(configCfg); file != "" {
				v.SetConfigFile(file)
				err := v.ReadInConfig()
				if err != nil {
					log.Fatalf("Error loading config file %q: %v", file, err)
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

	rootCmd.PersistentFlags().String(metricsLevelCfg, "BASIC", "Output level of telemetry metrics (NONE, BASIC, NORMAL, DETAILED)")
	v.BindPFlag(metricsLevelCfg, rootCmd.PersistentFlags().Lookup(metricsLevelCfg))

	rootCmd.PersistentFlags().Int(healthCheckHTTPPort, 13133, "Port on which to run the healthcheck http server.")
	v.BindPFlag(healthCheckHTTPPort, rootCmd.PersistentFlags().Lookup(healthCheckHTTPPort))

	// At least until we can use a generic, i.e.: OpenCensus, metrics exporter we default to Prometheus at port 8888, if not otherwise specified.
	rootCmd.PersistentFlags().Uint16(metricsPortCfg, 8888, "Port exposing collector telemetry.")
	v.BindPFlag(metricsPortCfg, rootCmd.PersistentFlags().Lookup(metricsPortCfg))

	// local flags
	rootCmd.Flags().StringVar(&config, configCfg, "", "Path to the config file")
	rootCmd.Flags().Bool(jaegerReceiverFlg, false,
		fmt.Sprintf("Flag to run the Jaeger receiver (i.e.: Jaeger Collector), default settings: %+v", *builder.NewDefaultJaegerReceiverCfg()))
	rootCmd.Flags().Bool(ocReceiverFlg, true,
		fmt.Sprintf("Flag to run the OpenCensus trace receiver, default settings: %+v", *builder.NewDefaultOpenCensusReceiverCfg()))
	rootCmd.Flags().Bool(zipkinReceiverFlg, false,
		fmt.Sprintf("Flag to run the Zipkin receiver, default settings: %+v", *builder.NewDefaultZipkinReceiverCfg()))
	rootCmd.Flags().Bool(debugProcessorFlg, false, "Flag to add a debug processor (combine with log level DEBUG to log incoming spans)")

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
	var asyncErrorChannel = make(chan error)
	var signalsChannel = make(chan os.Signal)
	signal.Notify(signalsChannel, os.Interrupt, syscall.SIGTERM)

	var err error
	logger, err = newLogger()
	if err != nil {
		log.Fatalf("Failed to get logger: %v", err)
	}

	telemetryLevel, err := telemetry.ParseLevel(v.GetString(metricsLevelCfg))
	if err != nil {
		log.Fatalf("Failed to parse metrics level: %v", err)
	}

	logger.Info("Starting...")

	hc, err := healthcheck.New(
		healthcheck.Unavailable, healthcheck.Logger(logger),
	).Serve(v.GetInt(healthCheckHTTPPort))
	if err != nil {
		log.Fatalf("Failed to start healthcheck server: %v", err)
	}

	// Build pipeline from its end: 1st exporters, the OC-proto queue processor, and
	// finally the receivers.

	var closeFns []func()
	var spanProcessors []processor.SpanProcessor
	exportersCloseFns, traceExporters, metricsExporters := createExporters()
	closeFns = append(closeFns, exportersCloseFns...)
	if len(traceExporters) > 0 {
		// Exporters need an extra hop from OC-proto to span data: to workaround that for now
		// we will use a special processor that transforms the data to a format that they can consume.
		// TODO: (@pjanotti) we should avoid this step in the long run, its an extra hop just to re-use
		// the exporters: this can lose node information and it is not ideal for performance and delegates
		// the retry/buffering to the exporters (that are designed to run within the tracing process).
		spanProcessors = append(spanProcessors, processor.NewTraceExporterProcessor(traceExporters...))
	}

	// TODO: (@pjanotti) make use of metrics exporters
	_ = metricsExporters

	if v.GetBool(debugProcessorFlg) {
		spanProcessors = append(spanProcessors, processor.NewNoopSpanProcessor(logger))
	}

	multiProcessorCfg := builder.NewDefaultMultiSpanProcessorCfg().InitFromViper(v)
	for _, queuedJaegerProcessorCfg := range multiProcessorCfg.Processors {
		logger.Info("Queued Jaeger Sender Enabled")
		queuedJaegerProcessor, err := buildQueuedSpanProcessor(logger, queuedJaegerProcessorCfg)
		if err != nil {
			logger.Error("Failed to build the queued span processor", zap.Error(err))
			os.Exit(1)
		}
		spanProcessors = append(spanProcessors, queuedJaegerProcessor)
	}

	if len(spanProcessors) == 0 {
		logger.Warn("Nothing to do: no processor was enabled. Shutting down.")
		os.Exit(1)
	}

	// Wraps processors in a single one to be connected to all enabled receivers.
	var processorOptions []processor.MultiProcessorOption
	if multiProcessorCfg.Global != nil && multiProcessorCfg.Global.Attributes != nil {
		logger.Info(
			"Found global attributes config",
			zap.Bool("overwrite", multiProcessorCfg.Global.Attributes.Overwrite),
			zap.Any("values", multiProcessorCfg.Global.Attributes.Values),
		)
		processorOptions = append(
			processorOptions,
			processor.WithAddAttributes(
				multiProcessorCfg.Global.Attributes.Values,
				multiProcessorCfg.Global.Attributes.Overwrite,
			),
		)
	}
	spanProcessor := processor.NewMultiSpanProcessor(spanProcessors, processorOptions...)

	receiversCloseFns := createReceivers(spanProcessor)
	closeFns = append(closeFns, receiversCloseFns...)

	err = initTelemetry(telemetryLevel, v.GetInt(metricsPortCfg), asyncErrorChannel, logger)
	if err != nil {
		logger.Error("Failed to initialize telemetry", zap.Error(err))
		os.Exit(1)
	}

	// mark service as ready to receive traffic.
	hc.Ready()

	select {
	case err = <-asyncErrorChannel:
		logger.Error("Asynchronous error received, terminating process", zap.Error(err))
	case <-signalsChannel:
		logger.Info("Received kill signal from OS")
	}

	logger.Info("Starting shutdown...")

	// TODO: orderly shutdown: first receivers, then flushing pipelines giving
	// senders a chance to send all their data. This may take time, the allowed
	// time should be part of configuration.
	for i := len(closeFns) - 1; i > 0; i-- {
		closeFns[i]()
	}

	logger.Info("Shutdown complete.")
}

func initTelemetry(level telemetry.Level, port int, asyncErrorChannel chan<- error, logger *zap.Logger) error {
	if level == telemetry.None {
		return nil
	}

	views := processor.MetricViews(level)
	views = append(views, processor.QueuedProcessorMetricViews(level)...)
	views = append(views, internal.AllViews...)
	if err := view.Register(views...); err != nil {
		return err
	}

	// Until we can use a generic metrics exporter, default to Prometheus.
	opts := prometheus.Options{
		Namespace: "oc_collector",
	}
	pe, err := prometheus.NewExporter(opts)
	if err != nil {
		return err
	}

	view.RegisterExporter(pe)

	logger.Info("Serving Prometheus metrics", zap.Int("port", port))
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", pe)
		serveErr := http.ListenAndServe(":"+strconv.Itoa(port), mux)
		if serveErr != nil && serveErr != http.ErrServerClosed {
			asyncErrorChannel <- serveErr
		}
	}()

	return nil
}

func createExporters() (doneFns []func(), traceExporters []exporter.TraceExporter, metricsExporters []exporter.MetricsExporter) {
	// TODO: (@pjanotti) this is slightly modified from agent but in the end duplication, need to consolidate style and visibility.
	parseFns := []struct {
		name string
		fn   func([]byte) ([]exporter.TraceExporter, []exporter.MetricsExporter, []func() error, error)
	}{
		{name: "datadog", fn: exporterparser.DatadogTraceExportersFromYAML},
		{name: "stackdriver", fn: exporterparser.StackdriverTraceExportersFromYAML},
		{name: "zipkin", fn: exporterparser.ZipkinExportersFromYAML},
		{name: "jaeger", fn: exporterparser.JaegerExportersFromYAML},
		{name: "kafka", fn: exporterparser.KafkaExportersFromYAML},
	}

	if config == "" {
		logger.Info("No config file, exporters can be only configured via the file.")
		return
	}

	cfgBlob, err := ioutil.ReadFile(config)
	if err != nil {
		logger.Fatal("Cannot read config file for exporters", zap.Error(err))
	}

	for _, cfg := range parseFns {
		tes, mes, tesDoneFns, err := cfg.fn(cfgBlob)
		if err != nil {
			logger.Fatal("Failed to create config for exporter", zap.String("exporter", cfg.name), zap.Error(err))
		}

		var anyTraceExporterEnabled, anyMetricsExporterEnabled bool

		for _, te := range tes {
			if te != nil {
				traceExporters = append(traceExporters, te)
				anyTraceExporterEnabled = true
			}
		}
		for _, me := range mes {
			if me != nil {
				metricsExporters = append(metricsExporters, me)
				anyMetricsExporterEnabled = true
			}
		}
		for _, tesDoneFn := range tesDoneFns {
			if tesDoneFn != nil {
				wrapperFn := func() {
					if err := tesDoneFn(); err != nil {
						logger.Warn("Error when closing exporter", zap.String("exporter", cfg.name), zap.Error(err))
					}
				}
				doneFns = append(doneFns, wrapperFn)
			}
		}

		if anyTraceExporterEnabled {
			logger.Info("Trace Exporter enabled", zap.String("exporter", cfg.name))
		}
		if anyMetricsExporterEnabled {
			logger.Info("Metrices Exporter enabled", zap.String("exporter", cfg.name))
		}

	}

	return doneFns, traceExporters, metricsExporters
}

func buildQueuedSpanProcessor(logger *zap.Logger, opts *builder.QueuedSpanProcessorCfg) (processor.SpanProcessor, error) {
	logger.Info("Constructing queue processor with name", zap.String("name", opts.Name))

	// build span batch sender from configured options
	var spanSender processor.SpanProcessor
	switch opts.SenderType {
	case builder.ThriftTChannelSenderType:
		logger.Info("Initializing thrift-tChannel sender")
		thriftTChannelSenderOpts := opts.SenderConfig.(*builder.JaegerThriftTChannelSenderCfg)
		tchrepbuilder := &tchReporter.Builder{
			CollectorHostPorts: thriftTChannelSenderOpts.CollectorHostPorts,
			DiscoveryMinPeers:  thriftTChannelSenderOpts.DiscoveryMinPeers,
			ConnCheckTimeout:   thriftTChannelSenderOpts.DiscoveryConnCheckTimeout,
		}
		tchreporter, err := tchrepbuilder.CreateReporter(metrics.NullFactory, logger)
		if err != nil {
			logger.Fatal("Cannot create tchannel reporter.", zap.Error(err))
			return nil, err
		}
		spanSender = sender.NewJaegerThriftTChannelSender(tchreporter, logger)
	case builder.ThriftHTTPSenderType:
		thriftHTTPSenderOpts := opts.SenderConfig.(*builder.JaegerThriftHTTPSenderCfg)
		logger.Info("Initializing thrift-HTTP sender",
			zap.String("url", thriftHTTPSenderOpts.CollectorEndpoint))
		spanSender = sender.NewJaegerThriftHTTPSender(
			thriftHTTPSenderOpts.CollectorEndpoint,
			thriftHTTPSenderOpts.Headers,
			logger,
			sender.HTTPTimeout(thriftHTTPSenderOpts.Timeout),
		)
	default:
		logger.Fatal("Unrecognized sender type configured")
	}

	// build queued span processor with underlying sender
	queuedSpanProcessor := processor.NewQueuedSpanProcessor(
		spanSender,
		processor.Options.WithLogger(logger),
		processor.Options.WithName(opts.Name),
		processor.Options.WithNumWorkers(opts.NumWorkers),
		processor.Options.WithQueueSize(opts.QueueSize),
		processor.Options.WithRetryOnProcessingFailures(opts.RetryOnFailure),
		processor.Options.WithBackoffDelay(opts.BackoffDelay),
	)
	return queuedSpanProcessor, nil
}

func createReceivers(spanProcessor processor.SpanProcessor) (closeFns []func()) {
	var someReceiverEnabled bool
	receivers := []struct {
		name    string
		runFn   func(*zap.Logger, *viper.Viper, processor.SpanProcessor) (func(), error)
		enabled bool
	}{
		{"Jaeger", jaegerreceiver.Run, builder.JaegerReceiverEnabled(v, jaegerReceiverFlg)},
		{"OpenCensus", ocreceiver.Run, builder.OpenCensusReceiverEnabled(v, ocReceiverFlg)},
		{"Zipkin", zipkinreceiver.Run, builder.ZipkinReceiverEnabled(v, zipkinReceiverFlg)},
	}

	for _, receiver := range receivers {
		if receiver.enabled {
			closeSrv, err := receiver.runFn(logger, v, spanProcessor)
			if err != nil {
				// TODO: (@pjanotti) better shutdown, for now just try to stop any started receiver before terminating.
				for _, closeFn := range closeFns {
					closeFn()
				}
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

	return closeFns
}

// Execute the application according to the command and configuration given
// by the user.
func Execute() error {
	return rootCmd.Execute()
}

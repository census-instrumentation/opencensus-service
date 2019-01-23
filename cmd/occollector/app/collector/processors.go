package collector

import (
	"io/ioutil"
	"os"

	tchReporter "github.com/jaegertracing/jaeger/cmd/agent/app/reporter/tchannel"
	"github.com/spf13/viper"
	"github.com/uber/jaeger-lib/metrics"
	"go.uber.org/zap"

	"github.com/census-instrumentation/opencensus-service/cmd/occollector/app/builder"
	"github.com/census-instrumentation/opencensus-service/cmd/occollector/app/sender"
	"github.com/census-instrumentation/opencensus-service/exporter"
	"github.com/census-instrumentation/opencensus-service/exporter/exporterparser"
	"github.com/census-instrumentation/opencensus-service/internal/collector/processor"
)

func createExporters(v *viper.Viper, logger *zap.Logger) (doneFns []func(), traceExporters []exporter.TraceExporter, metricsExporters []exporter.MetricsExporter) {
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

	config := builder.GetConfigFile(v)
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

func startProcessor(v *viper.Viper, logger *zap.Logger) (processor.SpanProcessor, []func()) {
	// Build pipeline from its end: 1st exporters, the OC-proto queue processor, and
	// finally the receivers.
	var closeFns []func()
	var spanProcessors []processor.SpanProcessor
	exportersCloseFns, traceExporters, metricsExporters := createExporters(v, logger)
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

	if builder.DebugProcessorEnabled(v) {
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
	return processor.NewMultiSpanProcessor(spanProcessors, processorOptions...), closeFns
}

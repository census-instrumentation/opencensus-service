package collector

import (
	"context"
	"os"

	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/census-instrumentation/opencensus-service/cmd/occollector/app/builder"
	"github.com/census-instrumentation/opencensus-service/internal/collector/jaeger"
	"github.com/census-instrumentation/opencensus-service/internal/collector/opencensus"
	"github.com/census-instrumentation/opencensus-service/internal/collector/processor"
	"github.com/census-instrumentation/opencensus-service/internal/collector/zipkin"
	"github.com/census-instrumentation/opencensus-service/receiver"
)

func createReceivers(v *viper.Viper, logger *zap.Logger, spanProcessor processor.SpanProcessor) []receiver.TraceReceiver {
	var someReceiverEnabled bool
	receivers := []struct {
		name    string
		runFn   func(*zap.Logger, *viper.Viper, processor.SpanProcessor) (receiver.TraceReceiver, error)
		enabled bool
	}{
		{"Jaeger", jaegerreceiver.Start, builder.JaegerReceiverEnabled(v)},
		{"OpenCensus", ocreceiver.Start, builder.OpenCensusReceiverEnabled(v)},
		{"Zipkin", zipkinreceiver.Start, builder.ZipkinReceiverEnabled(v)},
	}

	var startedTraceReceivers []receiver.TraceReceiver
	for _, receiver := range receivers {
		if receiver.enabled {
			rec, err := receiver.runFn(logger, v, spanProcessor)
			if err != nil {
				// TODO: (@pjanotti) better shutdown, for now just try to stop any started receiver before terminating.
				for _, startedTraceReceiver := range startedTraceReceivers {
					startedTraceReceiver.StopTraceReception(context.Background())
				}
				logger.Fatal("Cannot run receiver for "+receiver.name, zap.Error(err))
			}
			startedTraceReceivers = append(startedTraceReceivers, rec)
			someReceiverEnabled = true
		}
	}

	if !someReceiverEnabled {
		logger.Warn("Nothing to do: no receiver was enabled. Shutting down.")
		os.Exit(1)
	}

	return startedTraceReceivers
}

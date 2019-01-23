package collector

import (
	"flag"
	"log"
	"net/http"
	"strconv"

	"github.com/spf13/viper"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
	"go.uber.org/zap"

	"github.com/census-instrumentation/opencensus-service/internal"
	"github.com/census-instrumentation/opencensus-service/internal/collector/processor"
	"github.com/census-instrumentation/opencensus-service/internal/collector/telemetry"
)

const (
	metricsPortCfg  = "metrics-port"
	metricsLevelCfg = "metrics-level"
)

func telemetryFlags(flags *flag.FlagSet) {
	flags.String(metricsLevelCfg, "BASIC", "Output level of telemetry metrics (NONE, BASIC, NORMAL, DETAILED)")
	// At least until we can use a generic, i.e.: OpenCensus, metrics exporter we default to Prometheus at port 8888, if not otherwise specified.
	flags.Uint(metricsPortCfg, 8888, "Port exposing collector telemetry.")
}

func initTelemetry(asyncErrorChannel chan<- error, v *viper.Viper, logger *zap.Logger) error {
	level, err := telemetry.ParseLevel(v.GetString(metricsLevelCfg))
	if err != nil {
		log.Fatalf("Failed to parse metrics level: %v", err)
	}

	if level == telemetry.None {
		return nil
	}

	port := v.GetInt(metricsPortCfg)

	views := processor.MetricViews(level)
	views = append(views, processor.QueuedProcessorMetricViews(level)...)
	views = append(views, internal.AllViews...)
	processMetricsViews := telemetry.NewProcessMetricsViews()
	views = append(views, processMetricsViews.Views()...)
	if err := view.Register(views...); err != nil {
		return err
	}

	processMetricsViews.StartCollection()

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

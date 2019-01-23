package collector

import (
	"flag"

	"github.com/jaegertracing/jaeger/pkg/healthcheck"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

const (
	healthCheckHTTPPort = "health-check-http-port"
)

func healthCheckFlags(flags *flag.FlagSet) {
	flags.Uint(healthCheckHTTPPort, 13133, "Port on which to run the healthcheck http server.")
}

func newHealthCheck(v *viper.Viper, logger *zap.Logger) (*healthcheck.HealthCheck, error) {
	return healthcheck.New(
		healthcheck.Unavailable, healthcheck.Logger(logger),
	).Serve(v.GetInt(healthCheckHTTPPort))
}

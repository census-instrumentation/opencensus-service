package collector

import (
	"flag"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	logLevelCfg = "log-level"
)

func loggerFlags(flags *flag.FlagSet) {
	flags.String(logLevelCfg, "INFO", "Output level of logs (TRACE, DEBUG, INFO, WARN, ERROR, FATAL)")
}

func newLogger(v *viper.Viper) (*zap.Logger, error) {
	var level zapcore.Level
	err := (&level).UnmarshalText([]byte(v.GetString(logLevelCfg)))
	if err != nil {
		return nil, err
	}
	conf := zap.NewProductionConfig()
	conf.Level.SetLevel(level)
	return conf.Build()
}

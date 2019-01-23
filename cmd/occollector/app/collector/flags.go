package collector

import (
	"flag"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// addFlags adds the provided flags to the provided viper and cobra command.
func addFlags(v *viper.Viper, command *cobra.Command, addFlagsFns ...func(*flag.FlagSet)) (*viper.Viper, *cobra.Command) {
	flagSet := new(flag.FlagSet)
	for _, addFlags := range addFlagsFns {
		addFlags(flagSet)
	}
	command.Flags().AddGoFlagSet(flagSet)

	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.BindPFlags(command.Flags())
	return v, command
}

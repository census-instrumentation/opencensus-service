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

package builder

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

const (
	receiversRoot   = "receivers"
	jaegerEntry     = "jaeger"
	opencensusEntry = "opencensus"
	zipkinEntry     = "zipkin"
)

// JaegerReceiverCfg holds configuration for Jaeger receivers.
type JaegerReceiverCfg struct {
	// ThriftTChannelPort is the port that the relay receives on for jaeger thrift tchannel requests
	ThriftTChannelPort int `mapstructure:"jaeger-thrift-tchannel-port"`
	// ThriftHTTPPort is the port that the relay receives on for jaeger thrift http requests
	ThriftHTTPPort int `mapstructure:"jaeger-thrift-http-port"`
}

// JaegerReceiverEnabled checks if the Jaeger receiver is enabled, via a command-line flag, environment
// variable, or configuration file.
func JaegerReceiverEnabled(v *viper.Viper, cmdFlag string) bool {
	return featureEnabled(v, cmdFlag, receiversRoot, jaegerEntry)
}

// NewDefaultJaegerReceiverCfg returns an instance of JaegerReceiverCfg with default values
func NewDefaultJaegerReceiverCfg() *JaegerReceiverCfg {
	opts := &JaegerReceiverCfg{
		ThriftTChannelPort: 14267,
		ThriftHTTPPort:     14268,
	}
	return opts
}

// InitFromViper returns a JaegerReceiverCfg according to the configuration.
func (cfg *JaegerReceiverCfg) InitFromViper(v *viper.Viper) (*JaegerReceiverCfg, error) {
	return cfg, initFromViper(cfg, v, receiversRoot, jaegerEntry)
}

// OpenCensusReceiverCfg holds configuration for OpenCensus receiver.
type OpenCensusReceiverCfg struct {
	// Port is the port that the receiver will use
	Port int `mapstructure:"port"`
}

// OpenCensusReceiverEnabled checks if the OpenCensus receiver is enabled, via a command-line flag, environment
// variable, or configuration file.
func OpenCensusReceiverEnabled(v *viper.Viper, cmdFlag string) bool {
	return featureEnabled(v, cmdFlag, receiversRoot, opencensusEntry)
}

// NewDefaultOpenCensusReceiverCfg returns an instance of OpenCensusReceiverCfg with default values
func NewDefaultOpenCensusReceiverCfg() *OpenCensusReceiverCfg {
	opts := &OpenCensusReceiverCfg{
		Port: 55678,
	}
	return opts
}

// InitFromViper returns a OpenCensusReceiverCfg according to the configuration.
func (cfg *OpenCensusReceiverCfg) InitFromViper(v *viper.Viper) (*OpenCensusReceiverCfg, error) {
	return cfg, initFromViper(cfg, v, receiversRoot, opencensusEntry)
}

// ZipkinReceiverCfg holds configuration for Zipkin receiver.
type ZipkinReceiverCfg struct {
	// Port is the port that the receiver will use
	Port int `mapstructure:"port"`
}

// ZipkinReceiverEnabled checks if the Zipkin receiver is enabled, via a command-line flag, environment
// variable, or configuration file.
func ZipkinReceiverEnabled(v *viper.Viper, cmdFlag string) bool {
	return featureEnabled(v, cmdFlag, receiversRoot, zipkinEntry)
}

// NewDefaultZipkinReceiverCfg returns an instance of ZipkinReceiverCfg with default values
func NewDefaultZipkinReceiverCfg() *ZipkinReceiverCfg {
	opts := &ZipkinReceiverCfg{
		Port: 9411,
	}
	return opts
}

// InitFromViper returns a ZipkinReceiverCfg according to the configuration.
func (cfg *ZipkinReceiverCfg) InitFromViper(v *viper.Viper) (*ZipkinReceiverCfg, error) {
	return cfg, initFromViper(cfg, v, receiversRoot, zipkinEntry)
}

// Helper functions

func initFromViper(cfg interface{}, v *viper.Viper, labels ...string) error {
	v = getViperSub(v, labels...)
	if v == nil {
		return nil
	}
	if err := v.Unmarshal(cfg); err != nil {
		return fmt.Errorf("Failed to read configuration for %s %v", strings.Join(labels, ": "), err)
	}

	return nil
}

func getViperSub(v *viper.Viper, labels ...string) *viper.Viper {
	for _, label := range labels {
		v = v.Sub(label)
		if v == nil {
			return nil
		}
	}

	return v
}

func featureEnabled(v *viper.Viper, cmdFlag string, labels ...string) bool {
	return v.GetBool(cmdFlag) || (getViperSub(v, labels...) != nil)
}

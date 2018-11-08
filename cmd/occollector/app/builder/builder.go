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

// Package builder handles the options to build the OpenCensus collector
// pipeline.
package builder

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// JaegerReceiverCfg holds configuration for Jaeger receivers.
type JaegerReceiverCfg struct {
	// JaegerThriftTChannelPort is the port that the relay receives on for jaeger thrift tchannel requests
	JaegerThriftTChannelPort int `mapstructure:"thrift-tchannel-port"`
	// ReceiverJaegerHTTPPort is the port that the relay receives on for jaeger thrift http requests
	JaegerThriftHTTPPort int `mapstructure:"thrift-http-port"`
}

// NewJaegerReceiverCfg returns an instance of JaegerReceiverCfg with default values
func NewJaegerReceiverCfg() *JaegerReceiverCfg {
	opts := &JaegerReceiverCfg{
		JaegerThriftTChannelPort: 14267,
		JaegerThriftHTTPPort:     14268,
	}
	return opts
}

// InitFromViper returns a JaegerReceiverCfg according to the configuration.
func (cfg *JaegerReceiverCfg) InitFromViper(v *viper.Viper) (*JaegerReceiverCfg, error) {
	return cfg, initFromViper(cfg, v, "jaeger", "receiver")
}

// OpenCensusReceiverCfg holds configuration for OpenCensus receiver.
type OpenCensusReceiverCfg struct {
	// Port is the port that the receiver will use
	Port int `mapstructure:"port"`
}

// NewOpenCensusReceiverCfg returns an instance of OpenCensusReceiverCfg with default values
func NewOpenCensusReceiverCfg() *OpenCensusReceiverCfg {
	opts := &OpenCensusReceiverCfg{
		Port: 55678,
	}
	return opts
}

// InitFromViper returns a OpenCensusReceiverCfg according to the configuration.
func (cfg *OpenCensusReceiverCfg) InitFromViper(v *viper.Viper) (*OpenCensusReceiverCfg, error) {
	return cfg, initFromViper(cfg, v, "opencensus", "receiver")
}

// ZipkinReceiverCfg holds configuration for Zipkin receiver.
type ZipkinReceiverCfg struct {
	// Port is the port that the receiver will use
	Port int `mapstructure:"port"`
}

// NewZipkinReceiverCfg returns an instance of ZipkinReceiverCfg with default values
func NewZipkinReceiverCfg() *ZipkinReceiverCfg {
	opts := &ZipkinReceiverCfg{
		Port: 9411,
	}
	return opts
}

// InitFromViper returns a ZipkinReceiverCfg according to the configuration.
func (cfg *ZipkinReceiverCfg) InitFromViper(v *viper.Viper) (*ZipkinReceiverCfg, error) {
	return cfg, initFromViper(cfg, v, "zipkin", "receiver")
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
			break
		}
	}

	return v
}

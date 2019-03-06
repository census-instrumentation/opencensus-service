// Copyright 2019, OpenCensus Authors
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

// Package factorytemplate allows easy construction of factories for receivers.
// It requires the consumer to provide only functions to create the default
// configuration and how to create the receiver from that configuration.
package factorytemplate

import (
	"errors"

	"github.com/spf13/viper"

	"github.com/census-instrumentation/opencensus-service/receiver"
)

var (
	// ErrEmptyReciverType is returned when an empty name is given.
	ErrEmptyReciverType = errors.New("empty receiver type")
	// ErrNilNewDefaultCfg is returned when a nil newDefaultCfg is given.
	ErrNilNewDefaultCfg = errors.New("nil newDefaultCfg")
	// ErrNilNewReceiver is returned when a nil newReceiver is given.
	ErrNilNewReceiver = errors.New("nil newReceiver")
	// ErrNilViper is returned when the required viper parameter was nil.
	ErrNilViper = errors.New("nil Viper instance")
)

// factory implements the boiler-plate code used to create factories for receivers.
// Instead of implementing the interface directly one can only provide the parameters
// to contruct a new receiver factory.
type factory struct {
	receiverType  string
	newDefaultCfg func() interface{}
}

type traceReceiverFactory struct {
	factory
	newReceiver func(interface{}) (receiver.TraceReceiver, error)
}

var _ (receiver.TraceReceiverFactory) = (*traceReceiverFactory)(nil)

type metricsReceiverFactory struct {
	factory
	newReceiver func(interface{}) (receiver.MetricsReceiver, error)
}

var _ (receiver.MetricsReceiverFactory) = (*metricsReceiverFactory)(nil)

// NewTraceReceiverFactory creates a factory for the given receiver "type" that
// will have as the default configuration the object returned by newDefaultCfg and
// that can be created using newReceiver from the configuration type returned by
// newDefaultCfg.
//
// The object returned by newDefaultCfg should have its fields properly decorated using
// the mapstructure attribute.
//
// The first parameter passed to newReceiver is going to be from the same type returned by
// newDefaultCfg. The receiver implementer is going to be able to cast it to the appropriate
// type.
func NewTraceReceiverFactory(
	receiverType string,
	newDefaulfCfg func() interface{},
	newReceiver func(interface{}) (receiver.TraceReceiver, error),
) (receiver.TraceReceiverFactory, error) {
	if receiverType == "" {
		return nil, ErrEmptyReciverType
	}
	if newDefaulfCfg == nil {
		return nil, ErrNilNewDefaultCfg
	}
	if newReceiver == nil {
		return nil, ErrNilNewReceiver
	}

	return &traceReceiverFactory{
		factory: factory{
			receiverType:  receiverType,
			newDefaultCfg: newDefaulfCfg,
		},
		newReceiver: newReceiver,
	}, nil
}

// NewMetricsReceiverFactory creates a factory for the given receiver "type" that
// will have as the default configuration the object returned by newDefaultCfg and
// that can be created using newReceiver from the configuration type returned by
// newDefaultCfg.
//
// The object returned by newDefaultCfg should have its fields properly decorated using
// the mapstructure attribute.
//
// The first parameter passed to newReceiver is going to be from the same type returned by
// newDefaultCfg. The receiver implementer is going to be able to cast it to the appropriate
// type.
func NewMetricsReceiverFactory(
	receiverType string,
	newDefaulfCfg func() interface{},
	newReceiver func(interface{}) (receiver.MetricsReceiver, error),
) (receiver.MetricsReceiverFactory, error) {
	if receiverType == "" {
		return nil, ErrEmptyReciverType
	}
	if newDefaulfCfg == nil {
		return nil, ErrNilNewDefaultCfg
	}
	if newReceiver == nil {
		return nil, ErrNilNewReceiver
	}

	return &metricsReceiverFactory{
		factory: factory{
			receiverType:  receiverType,
			newDefaultCfg: newDefaulfCfg,
		},
		newReceiver: newReceiver,
	}, nil
}

// Type gets the type of the receiver created by this factory.
func (f *factory) Type() string {
	return f.receiverType
}

// NewFromViper takes a viper.Viper configuration and creates a new TraceReceiver.
// Returning also the configuration used to create it.
func (trf *traceReceiverFactory) NewFromViper(v *viper.Viper) (receiver.TraceReceiver, interface{}, error) {
	cfg, err := trf.configFromViper(v)
	if err != nil {
		return nil, nil, err
	}
	r, err := trf.newReceiver(cfg)
	if err != nil {
		return nil, nil, err
	}

	return r, cfg, nil
}

// NewFromViper takes a viper.Viper configuration and creates a new TraceReceiver.
// Returning also the configuration used to create it.
func (mrf *metricsReceiverFactory) NewFromViper(v *viper.Viper) (receiver.MetricsReceiver, interface{}, error) {
	cfg, err := mrf.configFromViper(v)
	if err != nil {
		return nil, nil, err
	}
	r, err := mrf.newReceiver(cfg)
	if err != nil {
		return nil, nil, err
	}

	return r, cfg, nil
}

// AddDefaultConfig returns the default configuration for the receiver
// created by this factory.
func (f *factory) AddDefaultConfig(v *viper.Viper) {
	v.SetDefault("receivers."+f.Type(), f.newDefaultCfg())
}

// configFromViper takes a viper.Viper, generates a default config and returns the
// resulting configuration.
func (f *factory) configFromViper(v *viper.Viper) (cfg interface{}, err error) {
	if v == nil {
		return nil, ErrNilViper
	}

	cfg = f.newDefaultCfg()
	err = v.Unmarshal(cfg)
	if err != nil {
		return nil, err
	}

	return cfg, err
}

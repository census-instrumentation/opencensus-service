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

package receiver

import (
	"github.com/spf13/viper"
)

// TraceReceiverFactory is an interface that builds a new TraceReceiver based on
// some viper.Viper configuration.
type TraceReceiverFactory interface {
	// GetType gets the type of the TraceReceiver created by this factory.
	GetType() string
	// NewFromViper takes a viper.Viper config and creates a new TraceReceiver.
	NewFromViper(cfg *viper.Viper) (TraceReceiver, error)
	// GetDefaultConfig returns the default configuration for TraceReceivers
	// created by this factory.
	GetDefaultConfig() *viper.Viper
}

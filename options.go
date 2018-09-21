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

package agent

import (
	"time"

	"github.com/census-instrumentation/opencensus-agent/internal/storage"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
)

type AgentOption interface {
	WithAgent(*Agent)
}

type storageOption struct {
	store storage.KeyValueStore
}

var _ AgentOption = (*storageOption)(nil)

func (so storageOption) WithAgent(ag *Agent) {
	ag.store = so.store
}

// WithKeyValueStore allows you to choose what key-value store to use to back the agent.
func WithKeyValueStore(store storage.KeyValueStore) AgentOption {
	return &storageOption{store: store}
}

type onTracesReceivedOption func(node *commonpb.Node, timeAt time.Time, recv *agenttracepb.ExportTraceServiceRequest, err error) error

var _ AgentOption = (*onTracesReceivedOption)(nil)

func (osro onTracesReceivedOption) WithAgent(a *Agent) {
	a.onTracesReceived = osro
}

// WithOnTracesReceivedFunc allows you to set an optional function that will be
// invoked on every received stream of traces from any connected agent-exporter.
func WithOnTracesReceivedFunc(fn func(*commonpb.Node, time.Time, *agenttracepb.ExportTraceServiceRequest, error) error) AgentOption {
	return onTracesReceivedOption(fn)
}

type onConfigResponseOption func(node *commonpb.Node, timeAt time.Time, res *agenttracepb.CurrentLibraryConfig, err error) error

var _ AgentOption = (*onConfigResponseOption)(nil)

func (ocro onConfigResponseOption) WithAgent(a *Agent) {
	a.onConfigResponse = ocro
}

// WithOnLibraryConfigReceivedFunc allows you to set an optional function that will be invoked
// on every received current library configuration from any connected agent-exporter.
func WithOnLibraryConfigReceivedFunc(fn func(*commonpb.Node, time.Time, *agenttracepb.CurrentLibraryConfig, error) error) AgentOption {
	return onConfigResponseOption(fn)
}

type onConfigNodeAcceptedOption func(node *commonpb.Node, timeAt time.Time)

func (ocna onConfigNodeAcceptedOption) WithAgent(a *Agent) {
	a.onConfigNodeAccepted = ocna
}

// WithOnConfigNodeAcceptedFunc allows you to set an optional function that will be invoked on
// every new Config connection, wherein the Node sent by the client will be passed to this function.
func WithOnConfigNodeAcceptedFunc(fn func(*commonpb.Node, time.Time)) AgentOption {
	return onConfigNodeAcceptedOption(fn)
}

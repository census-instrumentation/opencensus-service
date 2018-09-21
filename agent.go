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
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"

	"github.com/census-instrumentation/opencensus-agent/internal/storage"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
)

type Agent struct {
	// mu protects all the fields
	mu    sync.RWMutex
	store storage.KeyValueStore

	// onConfigNodeAccepted is the optional hook called when a client
	// connection is received and set as the first step in the Config method.
	onConfigNodeAccepted func(node *commonpb.Node, timeAt time.Time)

	// onTraceExportNodeConnected is the optional hook called when a client
	// connection is received and set as the first step in the Export method.
	onTraceExportNodeConnected func(node *commonpb.Node, timeAt time.Time)

	onConfigResponse func(node *commonpb.Node, timeAt time.Time, res *agenttracepb.CurrentLibraryConfig, err error) error
	onTracesReceived func(node *commonpb.Node, timeAt time.Time, recv *agenttracepb.ExportTraceServiceRequest, err error) error
}

func NewAgent(opts ...AgentOption) (*Agent, error) {
	ag := new(Agent)
	for _, opt := range opts {
		opt.WithAgent(ag)
	}
	if ag.store == nil {
		ag.store = storage.NewInMemoryKeyValueStore()
	}
	return ag, nil
}

var _ agenttracepb.TraceServiceServer = (*Agent)(nil)

// Config is the RPC method that the agent uses to stream trace configurations
// from its consumers (e.g. a web UI) down to each connected OpenCensus trace
// compatible library.
func (a *Agent) Config(tcs agenttracepb.TraceService_ConfigServer) error {
	// Firstly we MUST receive the node identifier to initiate
	// the service and start sending configurations.
	const maxConfigInitRetries = 10

	var node *commonpb.Node
	for i := 0; i < maxConfigInitRetries; i++ {
		recv, err := tcs.Recv()
		if err != nil {
			return err
		}

		if nd := recv.Node; nd != nil {
			node = nd
			break
		}
	}

	if node == nil {
		return fmt.Errorf("failed to receive non-nil Node even after %d retries", maxConfigInitRetries)
	}

	configsChan, done, err := a.registerConfigReceiver(node)
	if err != nil {
		return err
	}
	defer done()

	a.announceConfigNodeConnected(node, nowUTC())

	// And for the response loop
	go func() {
		for {
			recv, err := tcs.Recv()
			if err := a.processConfigResponse(node, nowUTC(), recv, err); err != nil {
				// TODO: Figure out how to report this error
				return
			}
		}
	}()

	for config := range configsChan {
		if err := tcs.Send(config); err != nil {
			return err
		}
	}

	return nil
}

// Export is the RPC method that receives stream traces
// from OpenCensus trace compability libraries.
func (a *Agent) Export(tes agenttracepb.TraceService_ExportServer) error {
	// Firstly we MUST receive the node identifier to initiate
	// the service and start accepting exported spans.
	const maxTraceInitRetries = 10

	var node *commonpb.Node
	for i := 0; i < maxTraceInitRetries; i++ {
		recv, err := tes.Recv()
		if err != nil {
			return err
		}

		if nd := recv.Node; nd != nil {
			node = nd
			break
		}
	}

	if node == nil {
		return fmt.Errorf("failed to receive non-nil Node even after %d retries", maxTraceInitRetries)
	}

	a.announceTraceExportNodeConnected(node, nowUTC())

	// Now that we've got the node, we can receive exported spans.
	for {
		recv, err := tes.Recv()
		if err := a.processTraces(node, nowUTC(), recv, err); err != nil {
			return err
		}
	}

	return nil
}

func nowUTC() time.Time { return time.Now().UTC() }

func (a *Agent) processTraces(node *commonpb.Node, timeAt time.Time, recv *agenttracepb.ExportTraceServiceRequest, err error) error {
	fn := a.onTracesReceived
	if fn == nil {
		// Since we have no spans handler set, just echo back the error
		return err
	}
	return fn(node, timeAt, recv, err)
}

func (a *Agent) processConfigResponse(node *commonpb.Node, timeAt time.Time, res *agenttracepb.CurrentLibraryConfig, err error) error {
	fn := a.onConfigResponse
	if fn == nil {
		// No config response handler sent so just echo back the error
		return err
	}
	return fn(node, timeAt, res, err)
}

func serializeAsKey(v proto.Message) (string, error) {
	pb, err := proto.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(pb), nil
}

func keyForConfig(v proto.Message) (string, error) {
	key, err := serializeAsKey(v)
	if err != nil {
		return "", err
	}
	return "key-for-config" + key, nil
}

type configReception struct {
	creationTime time.Time
	closeOnce    sync.Once
	configsChan  chan *agenttracepb.UpdatedLibraryConfig
	doneNotif    <-chan bool
	done         func()
}

// registerConfigReceiver registers a node and retrieves a channel on which configurations will be sent on
// to stream down to the application library. It also returns a function which MUST be run on exit to free
// resources associated with this work.
func (a *Agent) registerConfigReceiver(node *commonpb.Node) (configsChan <-chan *agenttracepb.UpdatedLibraryConfig, done func(), err error) {
	key, err := keyForConfig(node)
	if err != nil {
		return nil, nil, err
	}

	saved, ok, err := a.store.Get(key)
	if err == nil && ok && saved != nil {
		// Cache hit, we've already seen this node before.
		cfg := saved.(*configReception)
		return cfg.configsChan, cfg.done, nil
	}

	// This is the first time that we are seeing this node
	// for configuration reception, let's memoize it.
	cfgChan := make(chan *agenttracepb.UpdatedLibraryConfig)
	doneNotif := make(chan bool, 1)
	freshCfg := &configReception{
		creationTime: nowUTC(),
		configsChan:  cfgChan,
		doneNotif:    doneNotif,
	}
	freshCfg.done = func() {
		freshCfg.closeOnce.Do(func() {
			close(doneNotif)
			a.disconnectConfigNode(node)
		})
	}
	a.store.Put(key, freshCfg)

	return freshCfg.configsChan, freshCfg.done, nil
}

var (
	errNoRegisteredConfigReceiver = errors.New("no such registered config receiver")
	errConfigReceiverAlreadyGone  = errors.New("config receiver no longer exists")
)

func (a *Agent) SendConfig(node *commonpb.Node, config *agenttracepb.UpdatedLibraryConfig) error {
	key, err := keyForConfig(node)
	if err != nil {
		return err
	}

	saved, ok, err := a.store.Get(key)
	if err != nil {
		return err
	} else if !ok || saved == nil {
		return errNoRegisteredConfigReceiver
	}

	cfg := saved.(*configReception)

	// Otherwise now send the config or if the
	// receiver already went away return an error.
	select {
	case <-cfg.doneNotif:
		return errConfigReceiverAlreadyGone
	case cfg.configsChan <- config:
		// Successfully queued up the new configuration for sending.
		return nil
	}
}

func (a *Agent) disconnectConfigNode(node *commonpb.Node) bool {
	key, err := keyForConfig(node)
	if err != nil {
		return false
	}

	saved, ok, err := a.store.Get(key)
	if err == nil && saved != nil {
		a.store.Delete(key)
	}
	return ok
}

func (a *Agent) announceTraceExportNodeConnected(node *commonpb.Node, timeAt time.Time) {
	a.mu.Lock()
	fn := a.onTraceExportNodeConnected
	a.mu.Unlock()

	if fn != nil {
		fn(node, timeAt)
	}
}

func (a *Agent) announceConfigNodeConnected(node *commonpb.Node, timeAt time.Time) {
	a.mu.Lock()
	fn := a.onConfigNodeAccepted
	a.mu.Unlock()

	if fn != nil {
		fn(node, timeAt)
	}
}

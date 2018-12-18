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

package opencensus

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"google.golang.org/grpc"

	"github.com/census-instrumentation/opencensus-service/internal"
	"github.com/census-instrumentation/opencensus-service/receiver"
	"github.com/census-instrumentation/opencensus-service/receiver/opencensus/ocmetrics"
	"github.com/census-instrumentation/opencensus-service/receiver/opencensus/octrace"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"

	agentmetricspb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/metrics/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
)

// Receiver is the type that exposes Trace and Metrics reception.
type Receiver struct {
	mu         sync.Mutex
	grpcLn     net.Listener
	httpLn     net.Listener
	serverGRPC *grpc.Server
	serverHTTP *http.Server

	traceReceiverOpts   []octrace.Option
	metricsReceiverOpts []ocmetrics.Option

	traceReceiver   *octrace.Receiver
	metricsReceiver *ocmetrics.Receiver

	stopOnce                 sync.Once
	startGRPCServerOnce      sync.Once
	startHTTPServerOnce      sync.Once
	startTraceReceiverOnce   sync.Once
	startMetricsReceiverOnce sync.Once
}

var (
	errAlreadyStarted = errors.New("already started")
	errAlreadyStopped = errors.New("already stopped")
)

// New just creates the OpenCensus receiver services. It is the caller's
// responsibility to invoke the respective Start*Reception methods as well
// as the various Stop*Reception methods or simply Stop to end it.
func New(grpcAddr string, httpAddr string, opts ...Option) (*Receiver, error) {
	// TODO: (@odeke-em) use options to enable address binding changes.
	grpcLn, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return nil, fmt.Errorf("Failed to bind to gRPC address %q: %v", grpcAddr, err)
	}

	httpLn, err := net.Listen("tcp", httpAddr)
	if err != nil {
		return nil, fmt.Errorf("Failed to bind to HTTP address %q: %v", httpAddr, err)
	}

	ocr := &Receiver{grpcLn: grpcLn, httpLn: httpLn}

	for _, opt := range opts {
		opt.withReceiver(ocr)
	}

	return ocr, nil
}

// StartTraceReception exclusively runs the Trace receiver on the gRPC server.
// To start both Trace and Metrics receivers/services, please use Start.
func (ocr *Receiver) StartTraceReception(ctx context.Context, ts receiver.TraceReceiverSink) error {
	err := ocr.registerTraceReceiver(ts)
	if err != nil && err != errAlreadyStarted {
		return err
	}
	return ocr.startServers()
}

func (ocr *Receiver) registerTraceReceiver(ts receiver.TraceReceiverSink) error {
	var err = errAlreadyStarted

	ocr.startTraceReceiverOnce.Do(func() {
		ocr.traceReceiver, err = octrace.New(ts, ocr.traceReceiverOpts...)
		if err == nil {
			srv := ocr.grpcServer()
			agenttracepb.RegisterTraceServiceServer(srv, ocr.traceReceiver)
		}
	})

	return err
}

// StartMetricsReception exclusively runs the Metrics receiver on the gRPC server.
// To start both Trace and Metrics receivers/services, please use Start.
func (ocr *Receiver) StartMetricsReception(ctx context.Context, ms receiver.MetricsReceiverSink) error {
	err := ocr.registerMetricsReceiver(ms)
	if err != nil && err != errAlreadyStarted {
		return err
	}
	return ocr.startGRPCServer()
}

func (ocr *Receiver) registerMetricsReceiver(ms receiver.MetricsReceiverSink) error {
	var err = errAlreadyStarted

	ocr.startMetricsReceiverOnce.Do(func() {
		ocr.metricsReceiver, err = ocmetrics.New(ms, ocr.metricsReceiverOpts...)
		if err == nil {
			srv := ocr.grpcServer()
			agentmetricspb.RegisterMetricsServiceServer(srv, ocr.metricsReceiver)
		}
	})
	return err
}

func (ocr *Receiver) grpcServer() *grpc.Server {
	ocr.mu.Lock()
	defer ocr.mu.Unlock()

	if ocr.serverGRPC == nil {
		ocr.serverGRPC = internal.GRPCServerWithObservabilityEnabled()
	}

	return ocr.serverGRPC
}

func (ocr *Receiver) httpServer() *http.Server {
	ocr.mu.Lock()
	defer ocr.mu.Unlock()

	if ocr.serverHTTP == nil {
		ocr.serverHTTP = &http.Server{Handler: runtime.NewServeMux()}
	}

	return ocr.serverHTTP
}

// StopTraceReception is a method to turn off receiving traces. It
// currently is a noop because we don't yet know if gRPC allows
// stopping a specific service.
func (ocr *Receiver) StopTraceReception(ctx context.Context) error {
	// StopTraceReception is a noop currently.
	// TODO: (@odeke-em) investigate whether or not gRPC
	// provides a way to stop specific services.
	return nil
}

// StopMetricsReception is a method to turn off receiving metrics. It
// currently is a noop because we don't yet know if gRPC allows
// stopping a specific service.
func (ocr *Receiver) StopMetricsReception(ctx context.Context) error {
	// StopMetricsReception is a noop currently.
	// TODO: (@odeke-em) investigate whether or not gRPC
	// provides a way to stop specific services.
	return nil
}

// Start runs all the receivers/services namely, Trace and Metrics services.
func (ocr *Receiver) Start(ctx context.Context, ts receiver.TraceReceiverSink, ms receiver.MetricsReceiverSink) error {
	if err := ocr.registerTraceReceiver(ts); err != nil && err != errAlreadyStarted {
		return err
	}
	if err := ocr.registerMetricsReceiver(ms); err != nil && err != errAlreadyStarted {
		return err
	}

	if err := ocr.startServers(); err != nil && err != errAlreadyStarted {
		return err
	}

	// At this point we've successfully started all the services/receivers.
	// Add other start routines here.
	return nil
}

// Stop stops the underlying gRPC server and all the services running on it.
func (ocr *Receiver) Stop() error {
	ocr.mu.Lock()
	defer ocr.mu.Unlock()

	var err = errAlreadyStopped
	ocr.stopOnce.Do(func() {
		// TODO: (@odeke-em) should we instead do (*grpc.Server).GracefulStop?
		ocr.serverGRPC.Stop()
		_ = ocr.serverHTTP.Close()
		_ = ocr.grpcLn.Close()
		_ = ocr.httpLn.Close()
	})
	return err
}

// Starts the gRPC and HTTP servers in parallel to reduce startup time.
func (ocr *Receiver) startServers() error {
	var wg sync.WaitGroup
	wg.Add(2)
	var grpcErr error
	var httpErr error
	go func() {
		defer wg.Done()
		grpcErr = ocr.startGRPCServer()
	}()
	go func() {
		defer wg.Done()
		httpErr = ocr.startHTTPServer()
	}()
	wg.Wait()
	if grpcErr != nil {
		return grpcErr
	}
	if httpErr != nil {
		return httpErr
	}
	return nil
}

func (ocr *Receiver) startGRPCServer() error {
	err := errAlreadyStarted
	ocr.startGRPCServerOnce.Do(func() {
		errChan := make(chan error, 1)
		go func() {
			errChan <- ocr.serverGRPC.Serve(ocr.grpcLn)
		}()

		// Our goal is to heuristically try running the server
		// and if it returns an error immediately, we reporter that.
		select {
		case serr := <-errChan:
			err = serr

		case <-time.After(1 * time.Second):
			// No error otherwise returned in the period of 1s.
			// We can assume that the serve is at least running.
			err = nil
		}
	})
	return err
}

func (ocr *Receiver) startHTTPServer() error {
	err := errAlreadyStarted
	ocr.startHTTPServerOnce.Do(func() {
		errChan := make(chan error, 1)
		go func() {
			c := context.Background()
			opts := []grpc.DialOption{grpc.WithInsecure()}
			endpoint := ocr.grpcLn.Addr().String()
			mux := ocr.httpServer().Handler.(*runtime.ServeMux)
			err := agenttracepb.RegisterTraceServiceHandlerFromEndpoint(c, mux, endpoint, opts)
			if err != nil {
				errChan <- err
				return
			}
			errChan <- ocr.httpServer().Serve(ocr.httpLn)
		}()

		// Our goal is to heuristically try running the server
		// and if it returns an error immediately, we reporter that.
		select {
		case serr := <-errChan:
			err = serr

		case <-time.After(1 * time.Second):
			// No error otherwise returned in the period of 1s.
			// We can assume that the serve is at least running.
			err = nil
		}
	})
	return err
}

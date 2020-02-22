// Copyright 2020, OpenCensus Authors
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

package kairosdbexporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// restClient provides a simple implementation of an http client which can be used to invoke
// kairosdb rest api.
type restClient struct {
	logger              *zap.Logger
	endpoint            string
	logProducedMessages bool
	httpTransport       *http.Transport
	httpCli             *http.Client
}

// AddMetricsBuilder ingests all the metrics currently stored in the builder into KairosDB using
// the rest api.
func (cli *restClient) AddMetricsBuilder(ctx context.Context, metricsCollection *metricsBuilder) error {
	return cli.AddMetrics(ctx, metricsCollection.Build())
}

// AddMetrics ingests all the metrics currently stored in the collection into KairosDB using
// the rest api.
func (cli *restClient) AddMetrics(ctx context.Context, metricsCollection metrics) error {
	startTime := time.Now()
	cli.logger.Debug(fmt.Sprintf("Sending %v collection to endpoint %s", metricsCollection, cli.endpoint))

	body, err := json.Marshal(metricsCollection)
	if err != nil {
		cli.logger.Error(fmt.Sprintf("%v", err))
		return err
	}

	if cli.logProducedMessages {
		bodyStr := string(body)
		cli.logger.Debug(bodyStr)
	}

	buffer := bytes.NewBuffer(body)
	httpRequest, err := http.NewRequestWithContext(ctx, "POST", cli.endpoint, buffer)
	if err != nil {
		cli.logger.Error(fmt.Sprintf("Unexpected error while building the request instance: %v", err))
		return err
	}

	go func() {
		defer func() {
			endTime := time.Now()
			duration := endTime.Sub(startTime).Milliseconds()

			msg := fmt.Sprintf("Finish sending %v collection to endpoint %s after %d milliseconds.",
				metricsCollection, cli.endpoint, duration)
			cli.logger.Debug(msg)
		}()

		resp, err := cli.httpCli.Do(httpRequest)
		if err != nil {
			cli.logger.Error(fmt.Sprintf("Unexpected error while executing kairosdb request: %v", err))
			return
		}

		cli.logger.Debug(fmt.Sprintf("The status code received from kairosdb is: %d", resp.StatusCode))
	}()

	return nil
}

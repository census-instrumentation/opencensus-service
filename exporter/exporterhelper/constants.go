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

package exporterhelper

import (
	"errors"
)

var (
	// ErrEmptyExporterFormat is returned when an empty name is given.
	ErrEmptyExporterFormat = errors.New("empty exporter format")
	// ErrNilPushTraceData is returned when a nil pushTraceData is given.
	ErrNilPushTraceData = errors.New("nil pushTraceData")
	// ErrNilPushMetricsData is returned when a nil pushMetricsData is given.
	ErrNilPushMetricsData = errors.New("nil pushMetricsData")
)

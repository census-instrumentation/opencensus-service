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

package config

// TLSCredentials holds the fields for TLS credentials
// that are used for starting a server.
type TLSCredentials struct {
	// CertFile is the file path containing the TLS certificate.
	CertFile string `yaml:"cert_file"`

	// KeyFile is the file path containing the TLS key.
	KeyFile string `yaml:"key_file"`
}

// nonEmpty returns true if the TLSCredentials are non-nil and
// if either CertFile or KeyFile is non-empty.
func (tc *TLSCredentials) nonEmpty() bool {
	return tc != nil && (tc.CertFile != "" || tc.KeyFile != "")
}

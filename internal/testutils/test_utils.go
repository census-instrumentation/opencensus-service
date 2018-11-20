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

package testutils

import (
	"encoding/json"
	"io/ioutil"
)

// GenerateNormalizedJSON generates a normalized JSON from the string
// given to the function. Useful to compare JSON contents that
// may have differences due to formatting. It returns nil in case of
// invalid JSON.
func GenerateNormalizedJSON(j string) string {
	var i interface{}
	json.Unmarshal([]byte(j), &i)
	n, _ := json.Marshal(i)
	return string(n)
}

// SaveAsFormattedJSON save the object as a formatted JSON file to help
// with investigations.
func SaveAsFormattedJSON(file string, o interface{}) error {
	blob, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return nil
	}
	return ioutil.WriteFile(file, blob, 0644)
}

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

package configv2

import (
	"bytes"
	"fmt"

	"github.com/spf13/viper"
)

// readConfigFromYamlStr reads a Viper config from a string in YAML format.
func readConfigFromYamlStr(str string) (*viper.Viper, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	err := v.ReadConfig(bytes.NewBuffer([]byte(str)))
	if err != nil {
		return nil, err
	}
	return v, nil
}

// getViperSubSequenceOfMaps extracts a subsequence of maps from a configuration
// It is expected that v has the following structure:
//
// key:
//   - key1: val1
//     key2: val2
//     ... more key/values
//   - key1: val1
//     key2: val2
//     ... more key/values
//
// Each sequence element is returned as a sub Viper that can be used normally
// to query containing key/values.
func getViperSubSequenceOfMaps(v *viper.Viper, key string) ([]*viper.Viper, error) {
	subs := make([]*viper.Viper, 0)
	childData := v.Get(key)
	if childData == nil {
		// There is no children with specified key
		return nil, fmt.Errorf("cannot find key %q", key)
	}

	childDataIterable, ok := childData.([]interface{})
	if !ok {
		// The children is not a sequence
		return nil, fmt.Errorf("key %q must be a sequence", key)
	}

	for i, item := range childDataIterable {
		itemAsMap, ok := item.(map[interface{}]interface{})
		if !ok {
			// Value is not a map, ignore it
			return nil, fmt.Errorf("element at index %d under key %q is not a key/value map", i, key)
		}

		// Create a new sub Viper for this element of sequence
		sub := viper.New()

		// Add all key/value pairs of this element to the sub Viper
		for key, val := range itemAsMap {
			keyStr := key.(string)
			sub.Set(keyStr, val)
		}
		subs = append(subs, sub)
	}
	return subs, nil
}

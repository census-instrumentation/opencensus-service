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

package builder

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Helper functions

func initFromViper(cfg interface{}, v *viper.Viper, labels ...string) error {
	v = getViperSub(v, labels...)
	if v == nil {
		return nil
	}
	if err := v.Unmarshal(cfg); err != nil {
		return fmt.Errorf("Failed to read configuration for %s %v", strings.Join(labels, ": "), err)
	}

	return nil
}

func getViperSub(v *viper.Viper, labels ...string) *viper.Viper {
	for _, label := range labels {
		v = v.Sub(label)
		if v == nil {
			return nil
		}
	}

	return v
}

func featureEnabled(v *viper.Viper, cmdFlag string, labels ...string) bool {
	return v.GetBool(cmdFlag) || (getViperSub(v, labels...) != nil)
}

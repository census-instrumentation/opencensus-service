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

package internal

import (
	"testing"
)

func Test_mCache_EvictUnused(t *testing.T) {
	mc := &mCache{startValues: make(map[string]*ObservedValue), accessTimes: 1}
	mc.StoreObservedValue("k1", startTs, 10)
	if l := mc.startValues["k1"].lastAccessRun; l != 1 {
		t.Errorf("expect lastAccessRun to be 1, but get %v", l)
	}

	mc.accessTimes = 2
	mc.LoadObservedValue("k1")
	if l := mc.startValues["k1"].lastAccessRun; l != 2 {
		t.Errorf("expect lastAccessRun to be 2, but get %v", l)
	}

	mc.accessTimes = 5
	mc.EvictUnused()
	if len(mc.startValues) != 1 {
		t.Error("not expect cached value to be evicted")
	}

	mc.accessTimes = 6
	mc.EvictUnused()
	if len(mc.startValues) != 0 {
		t.Error("expect cached value to be evicted")
	}

}

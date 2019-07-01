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
	"errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/scrape"
	"sync"
)

const maxUnusedRuns = 3

// MetadataService is an adapter to scrapeManager and provide only the functionality which is needed
type MetadataService interface {
	Get(job, instance string) (MetadataCache, error)
}

// MetadataCache is an adapter to prometheus' scrape.Target  and provide only the functionality which is needed
type MetadataCache interface {
	Metadata(metricName string) (scrape.MetricMetadata, bool)
	SharedLabels() labels.Labels
	LoadObservedValue(key string) (*ObservedValue, bool)
	StoreObservedValue(key string, ts int64, value float64)
	LastScrapeTime() int64
	EvictUnused()
}

type mService struct {
	sm    *scrape.Manager
	cache sync.Map
}

func (ms *mService) Get(job, instance string) (MetadataCache, error) {
	cacheKey := job + "|" + instance
	c, ok := ms.cache.Load(cacheKey)
	if ok {
		mc := c.(*mCache)
		mc.accessTimes++
		return mc, nil
	}

	targetGroup, ok := ms.sm.TargetsAll()[job]
	if !ok {
		return nil, errors.New("unable to find a target group with job=" + job)
	}

	// from the same targetGroup, instance won't be duplicated
	for _, target := range targetGroup {
		if target.DiscoveredLabels().Get(model.AddressLabel) == instance {
			mc := &mCache{t: target, startValues: make(map[string]*ObservedValue), accessTimes: 1}
			ms.cache.Store(cacheKey, mc)
			return mc, nil
		}
	}

	return nil, errors.New("unable to find a target with job=" + job + ", and instance=" + instance)
}

// ObservedValue is used to store the start time, first observed value and last seen value of cumulative data
type ObservedValue struct {
	Ts            int64
	StartValue    float64
	LastSeen      float64
	lastAccessRun int64
}

var dummyObservedValue = &ObservedValue{}

// a structure that caches a scrape target and some other metadata related to the target
type mCache struct {
	t           *scrape.Target
	startValues map[string]*ObservedValue
	accessTimes int64
}

// EvictUnused is used at the end of each scrape run for removing cached startValues which are not accessed in the past
// maxUnusedRuns, as metrics can come and go from a scrape target endpoint
func (m *mCache) EvictUnused() {
	for k, v := range m.startValues {
		if m.accessTimes-v.lastAccessRun > maxUnusedRuns {
			delete(m.startValues, k)
		}
	}
}

func (m *mCache) LoadObservedValue(key string) (*ObservedValue, bool) {
	v, ok := m.startValues[key]
	if ok {
		v.lastAccessRun = m.accessTimes
		return v, true
	}
	return dummyObservedValue, false
}

func (m *mCache) StoreObservedValue(key string, ts int64, value float64) {
	m.startValues[key] = &ObservedValue{Ts: ts, StartValue: value, LastSeen: value, lastAccessRun: m.accessTimes}
}

func (m *mCache) LastScrapeTime() int64 {
	t := m.t.LastScrape()
	// empty time is: 0001-01-01 00:00:00 +0000 UTC which cannot be represent by int64
	if t.IsZero() {
		return int64(0)
	}
	return t.UnixNano() / int64(1000000)
}

func (m *mCache) Metadata(metricName string) (scrape.MetricMetadata, bool) {
	return m.t.Metadata(metricName)
}

func (m *mCache) SharedLabels() labels.Labels {
	return m.t.DiscoveredLabels()
}

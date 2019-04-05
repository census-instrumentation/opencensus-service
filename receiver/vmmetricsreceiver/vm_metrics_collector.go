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

package vmmetricsreceiver

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/prometheus/procfs"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"

	"github.com/census-instrumentation/opencensus-service/consumer"
	"github.com/census-instrumentation/opencensus-service/data"

	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
)

// VMMetricsCollector is a struct that contains views related to VM and process metrics (cpu, mem, etc),
// collects and reports metrics for those views.
type VMMetricsCollector struct {
	consumer consumer.MetricsConsumer

	startTime      time.Time
	views          []*view.View
	fs             procfs.FS
	scrapeInterval time.Duration
	metricPrefix   string
	done           chan struct{}
}

const (
	defaultMountPoint = procfs.DefaultMountPoint // "/proc"
	defaultScrapeInterval = 10 * time.Second
)

// TODO(songy23): remove all measures and views and use Metrics APIs instead.

var mRuntimeAllocMem = stats.Int64("process/memory_alloc", "Number of bytes currently allocated in use", "By")
var viewAllocMem = &view.View{
	Name:        mRuntimeAllocMem.Name(),
	Description: mRuntimeAllocMem.Description(),
	Measure:     mRuntimeAllocMem,
	Aggregation: view.LastValue(),
	TagKeys:     nil,
}

var mRuntimeTotalAllocMem = stats.Int64("process/total_memory_alloc", "Number of allocations in total", "By")
var viewTotalAllocMem = &view.View{
	Name:        mRuntimeTotalAllocMem.Name(),
	Description: mRuntimeTotalAllocMem.Description(),
	Measure:     mRuntimeTotalAllocMem,
	Aggregation: view.LastValue(),
	TagKeys:     nil,
}

var mRuntimeSysMem = stats.Int64("process/sys_memory_alloc", "Number of bytes given to the process to use in total", "By")
var viewSysMem = &view.View{
	Name:        mRuntimeSysMem.Name(),
	Description: mRuntimeSysMem.Description(),
	Measure:     mRuntimeSysMem,
	Aggregation: view.LastValue(),
	TagKeys:     nil,
}

var mCPUSeconds = stats.Int64("process/cpu_seconds", "CPU seconds for this process", "1")
var viewCPUSeconds = &view.View{
	Name:        mCPUSeconds.Name(),
	Description: mCPUSeconds.Description(),
	Measure:     mCPUSeconds,
	Aggregation: view.LastValue(),
	TagKeys:     nil,
}

var mUserCPUSeconds = stats.Float64("system/cpu_seconds/user", "Total kernel/system user CPU seconds", "s")
var viewUserCPUSeconds = &view.View{
	Name:        mUserCPUSeconds.Name(),
	Description: mUserCPUSeconds.Description(),
	Measure:     mUserCPUSeconds,
	Aggregation: view.Sum(),
	TagKeys:     nil,
}

var mSystemCPUSeconds = stats.Float64("system/cpu_seconds/system", "Total kernel/system system CPU seconds", "s")
var viewSystemCPUSeconds = &view.View{
	Name:        mSystemCPUSeconds.Name(),
	Description: mSystemCPUSeconds.Description(),
	Measure:     mSystemCPUSeconds,
	Aggregation: view.Sum(),
	TagKeys:     nil,
}

var mIdleCPUSeconds = stats.Float64("system/cpu_seconds/idle", "Total kernel/system idle CPU seconds", "s")
var viewIdleCPUSeconds = &view.View{
	Name:        mIdleCPUSeconds.Name(),
	Description: mIdleCPUSeconds.Description(),
	Measure:     mIdleCPUSeconds,
	Aggregation: view.Sum(),
	TagKeys:     nil,
}

var views = []*view.View{viewAllocMem, viewTotalAllocMem, viewSysMem, viewCPUSeconds, viewUserCPUSeconds, viewSystemCPUSeconds, viewIdleCPUSeconds}

// NewVMMetricsCollector creates a new set of ProcessMetrics (mem, cpu) that can be used to measure
// basic information about this process.
func NewVMMetricsCollector(si time.Duration, mpoint, mprefix string, consumer consumer.MetricsConsumer) (*VMMetricsCollector, error) {
	if mpoint == "" {
		mpoint = defaultMountPoint
	}
	if si <= 0 {
		si = defaultScrapeInterval
	}
	fs, err := procfs.NewFS(mpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create new VMMetricsCollector: %s", err)
	}
	vmc := &VMMetricsCollector{
		consumer:       consumer,
		startTime:      time.Now(),
		views:          views,
		fs:             fs,
		scrapeInterval: si,
		metricPrefix:   mprefix,
		done:           make(chan struct{}),
	}
	view.Register(vmc.views...)
	return vmc, nil
}

// StartCollection starts a ticker'd goroutine that will update the PMV measurements every 5 seconds
func (vmc *VMMetricsCollector) StartCollection() {
	ticker := time.NewTicker(vmc.scrapeInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				vmc.scrape()
				vmc.export()

			case <-vmc.done:
				return
			}
		}
	}()
}

// StopCollection stops the collection of the process metric information
func (vmc *VMMetricsCollector) StopCollection() {
	close(vmc.done)
}

func (vmc *VMMetricsCollector) scrape() {
	ms := &runtime.MemStats{}
	runtime.ReadMemStats(ms)
	stats.Record(context.Background(), mRuntimeAllocMem.M(int64(ms.Alloc)))
	stats.Record(context.Background(), mRuntimeTotalAllocMem.M(int64(ms.TotalAlloc)))
	stats.Record(context.Background(), mRuntimeSysMem.M(int64(ms.Sys)))

	pid := os.Getpid()
	proc, err := procfs.NewProc(pid)
	if err == nil {
		if procStat, err := proc.NewStat(); err == nil {
			stats.Record(context.Background(), mCPUSeconds.M(int64(procStat.CPUTime())))
		}
	}

	if stat, err := vmc.fs.NewStat(); err == nil {
		cpuStat := stat.CPUTotal
		stats.Record(context.Background(), mUserCPUSeconds.M(cpuStat.User))
		stats.Record(context.Background(), mSystemCPUSeconds.M(cpuStat.System))
		stats.Record(context.Background(), mIdleCPUSeconds.M(cpuStat.Idle))
	}
}

func (vmc *VMMetricsCollector) export() {
	vds := []*view.Data{}
	for _, v := range vmc.views {
		if rows, err := view.RetrieveData(v.Name); err == nil {
			vd := view.Data{
				View:  v,
				Start: vmc.startTime,
				End:   time.Now(),
				Rows:  rows,
			}
			vds = append(vds, &vd)
		}
	}
	vmc.uploadViewData(vds)
}

func (vmc *VMMetricsCollector) uploadViewData(vds []*view.Data) {
	if len(vds) == 0 {
		return
	}

	ctx, span := trace.StartSpan(context.Background(), "VMMetricsCollector.uploadViewData")
	defer span.End()

	metrics := make([]*metricspb.Metric, 0, len(vds))
	for _, vd := range vds {
		if metric, err := viewDataToMetric(vd); err == nil {
			metrics = append(metrics, metric)
		}
	}
	vmc.consumer.ConsumeMetricsData(ctx, data.MetricsData{Metrics: metrics})
}

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
	defaultMountPoint     = procfs.DefaultMountPoint // "/proc"
	defaultScrapeInterval = 10 * time.Second
)

// NewVMMetricsCollector creates a new set of VM and Process Metrics (mem, cpu).
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
		views:          vmViews,
		fs:             fs,
		scrapeInterval: si,
		metricPrefix:   mprefix,
		done:           make(chan struct{}),
	}
	view.Register(vmc.views...)
	return vmc, nil
}

// StartCollection starts a ticker'd goroutine that will scrape and export vm metrics periodically.
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

// StopCollection stops the collection of metric information
func (vmc *VMMetricsCollector) StopCollection() {
	close(vmc.done)
}

func (vmc *VMMetricsCollector) scrape() {
	ms := &runtime.MemStats{}
	runtime.ReadMemStats(ms)
	ctx := context.Background()
	stats.Record(ctx, mRuntimeAllocMem.M(int64(ms.Alloc)))
	stats.Record(ctx, mRuntimeTotalAllocMem.M(int64(ms.TotalAlloc)))
	stats.Record(ctx, mRuntimeSysMem.M(int64(ms.Sys)))

	pid := os.Getpid()
	proc, err := procfs.NewProc(pid)
	if err == nil {
		if procStat, err := proc.NewStat(); err == nil {
			stats.Record(ctx, mCPUSeconds.M(int64(procStat.CPUTime())))
		}
	}

	if stat, err := vmc.fs.NewStat(); err == nil {
		stats.Record(ctx, mProcessesCreated.M(int64(stat.ProcessCreated)))
		stats.Record(ctx, mProcessesRunning.M(int64(stat.ProcessesRunning)))
		stats.Record(ctx, mProcessesBlocked.M(int64(stat.ProcessesBlocked)))

		cpuStat := stat.CPUTotal
		stats.Record(ctx, mUserCPUSeconds.M(cpuStat.User))
		stats.Record(ctx, mNiceCPUSeconds.M(cpuStat.Nice))
		stats.Record(ctx, mSystemCPUSeconds.M(cpuStat.System))
		stats.Record(ctx, mIdleCPUSeconds.M(cpuStat.Idle))
		stats.Record(ctx, mIowaitCPUSeconds.M(cpuStat.Iowait))
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

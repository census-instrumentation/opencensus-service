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
	"go.opencensus.io/trace"

	"github.com/census-instrumentation/opencensus-service/consumer"
	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/internal"

	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
)

// VMMetricsCollector is a struct that contains views related to VM and process metrics (cpu, mem, etc),
// collects and reports metrics for those views.
type VMMetricsCollector struct {
	consumer consumer.MetricsConsumer

	startTime time.Time

	fs        procfs.FS
	processFs procfs.FS

	scrapeInterval time.Duration
	metricPrefix   string
	done           chan struct{}
}

const (
	defaultMountPoint     = procfs.DefaultMountPoint // "/proc"
	defaultScrapeInterval = 10 * time.Second
)

// NewVMMetricsCollector creates a new set of VM and Process Metrics (mem, cpu).
func NewVMMetricsCollector(si time.Duration, mountPoint, processMountPoint, prefix string, consumer consumer.MetricsConsumer) (*VMMetricsCollector, error) {
	if mountPoint == "" {
		mountPoint = defaultMountPoint
	}
	if processMountPoint == "" {
		processMountPoint = defaultMountPoint
	}
	if si <= 0 {
		si = defaultScrapeInterval
	}
	fs, err := procfs.NewFS(mountPoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create new VMMetricsCollector: %s", err)
	}
	processFs, err := procfs.NewFS(processMountPoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create new VMMetricsCollector: %s", err)
	}
	vmc := &VMMetricsCollector{
		consumer:       consumer,
		startTime:      time.Now(),
		fs:             fs,
		processFs:      processFs,
		scrapeInterval: si,
		metricPrefix:   prefix,
		done:           make(chan struct{}),
	}

	return vmc, nil
}

// StartCollection starts a ticker'd goroutine that will scrape and export vm metrics periodically.
func (vmc *VMMetricsCollector) StartCollection() {
	go func() {
		ticker := time.NewTicker(vmc.scrapeInterval)
		var prevProcStat *procfs.ProcStat
		var prevStat *procfs.Stat
		for {
			select {
			case <-ticker.C:
				prevProcStat, prevStat = vmc.scrapeAndExport(prevProcStat, prevStat)

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

func (vmc *VMMetricsCollector) scrapeAndExport(prevProcStat *procfs.ProcStat, prevStat *procfs.Stat) (*procfs.ProcStat, *procfs.Stat) {
	ctx, span := trace.StartSpan(context.Background(), "VMMetricsCollector.scrapeAndExport")
	defer span.End()

	metrics := make([]*metricspb.Metric, 0, len(vmMetricDescriptors))
	var errs []error

	ms := &runtime.MemStats{}
	runtime.ReadMemStats(ms)
	metrics = append(
		metrics,
		&metricspb.Metric{
			MetricDescriptor: metricAllocMem,
			Timeseries:       []*metricspb.TimeSeries{vmc.getInt64TimeSeries(ms.Alloc)},
		},
		&metricspb.Metric{
			MetricDescriptor: metricTotalAllocMem,
			Timeseries:       []*metricspb.TimeSeries{vmc.getInt64TimeSeries(ms.TotalAlloc)},
		},
		&metricspb.Metric{
			MetricDescriptor: metricSysMem,
			Timeseries:       []*metricspb.TimeSeries{vmc.getInt64TimeSeries(ms.Sys)},
		},
	)

	pid := os.Getpid()
	var proc procfs.Proc
	var err error
	proc, err = vmc.processFs.NewProc(pid)
	if err == nil {
		procStat, err := proc.NewStat()
		if err == nil {
			var processCPUTimeDelta float64
			if prevProcStat != nil {
				processCPUTimeDelta = procStat.CPUTime() - prevProcStat.CPUTime()
			} else {
				processCPUTimeDelta = procStat.CPUTime()
			}
			metrics = append(
				metrics,
				&metricspb.Metric{
					MetricDescriptor: metricProcessCPUSeconds,
					Timeseries:       []*metricspb.TimeSeries{vmc.getDoubleTimeSeries(processCPUTimeDelta, nil)},
				},
			)
		}
		prevProcStat = &procStat
	}

	if err != nil {
		errs = append(errs, err)
	}

	stat, err := vmc.fs.NewStat()
	if err == nil {
		cpuStat := stat.CPUTotal
		var processCreatedDelta uint64
		var cpuTimeUserDelta, cpuTimeSystemDelta, cpuTimeIdleDelta, cpuTimeNiceDelta, cpuTimeIOWaitDelta float64
		if prevStat != nil {
			processCreatedDelta = stat.ProcessCreated - prevStat.ProcessCreated
			cpuTimeUserDelta = cpuStat.User - prevStat.CPUTotal.User
			cpuTimeSystemDelta = cpuStat.System - prevStat.CPUTotal.System
			cpuTimeIdleDelta = cpuStat.Idle - prevStat.CPUTotal.Idle
			cpuTimeNiceDelta = cpuStat.Nice - prevStat.CPUTotal.Nice
			cpuTimeIOWaitDelta = cpuStat.Iowait - prevStat.CPUTotal.Iowait
		} else {
			processCreatedDelta = stat.ProcessCreated
			cpuTimeUserDelta = cpuStat.User
			cpuTimeSystemDelta = cpuStat.System
			cpuTimeIdleDelta = cpuStat.Idle
			cpuTimeNiceDelta = cpuStat.Nice
			cpuTimeIOWaitDelta = cpuStat.Iowait
		}

		metrics = append(
			metrics,
			&metricspb.Metric{
				MetricDescriptor: metricProcessesRunning,
				Timeseries:       []*metricspb.TimeSeries{vmc.getInt64TimeSeries(stat.ProcessesRunning)},
			},
			&metricspb.Metric{
				MetricDescriptor: metricProcessesBlocked,
				Timeseries:       []*metricspb.TimeSeries{vmc.getInt64TimeSeries(stat.ProcessesBlocked)},
			},
			&metricspb.Metric{
				MetricDescriptor: metricProcessesCreated,
				Timeseries:       []*metricspb.TimeSeries{vmc.getInt64TimeSeries(processCreatedDelta)},
			},
			&metricspb.Metric{
				MetricDescriptor: metricCPUSeconds,
				Timeseries: []*metricspb.TimeSeries{
					vmc.getDoubleTimeSeries(cpuTimeUserDelta, labelValueCPUUser),
					vmc.getDoubleTimeSeries(cpuTimeSystemDelta, labelValueCPUSystem),
					vmc.getDoubleTimeSeries(cpuTimeIdleDelta, labelValueCPUIdle),
					vmc.getDoubleTimeSeries(cpuTimeNiceDelta, labelValueCPUNice),
					vmc.getDoubleTimeSeries(cpuTimeIOWaitDelta, labelValueCPUIOWait),
				},
			},
		)

		prevStat = &stat
	} else {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		span.SetStatus(trace.Status{Code: 15 /*DATA_LOSS*/, Message: fmt.Sprintf("Error(s) when scraping VM metrics: %v", errs)})
	}

	if len(metrics) > 0 {
		vmc.consumer.ConsumeMetricsData(ctx, data.MetricsData{Metrics: metrics})
	}

	return prevProcStat, prevStat
}

func (vmc *VMMetricsCollector) getInt64TimeSeries(val uint64) *metricspb.TimeSeries {
	return &metricspb.TimeSeries{
		StartTimestamp: internal.TimeToTimestamp(vmc.startTime),
		Points:         []*metricspb.Point{{Timestamp: internal.TimeToTimestamp(time.Now()), Value: &metricspb.Point_Int64Value{Int64Value: int64(val)}}},
	}
}

func (vmc *VMMetricsCollector) getDoubleTimeSeries(val float64, labelVal *metricspb.LabelValue) *metricspb.TimeSeries {
	labelVals := []*metricspb.LabelValue{}
	if labelVal != nil {
		labelVals = append(labelVals, labelVal)
	}
	return &metricspb.TimeSeries{
		StartTimestamp: internal.TimeToTimestamp(vmc.startTime),
		LabelValues:    labelVals,
		Points:         []*metricspb.Point{{Timestamp: internal.TimeToTimestamp(time.Now()), Value: &metricspb.Point_DoubleValue{DoubleValue: val}}},
	}
}

package telemetry

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/prometheus/procfs"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

// ProcessMetricsViews is a struct that contains views related to process metrics (cpu, mem, etc)
type ProcessMetricsViews struct {
	views []*view.View
	done  chan struct{}
}

var mRuntimeAllocMem = stats.Int64("oc.io/process/memory_alloc", "Number of bytes currently allocated in use", "By")
var viewAllocMem = &view.View{
	Name:        mRuntimeAllocMem.Name(),
	Description: mRuntimeAllocMem.Description(),
	Measure:     mRuntimeAllocMem,
	Aggregation: view.LastValue(),
	TagKeys:     []tag.Key{},
}

var mRuntimeTotalAllocMem = stats.Int64("oc.io/process/total_memory_alloc", "Number of bytes allocated in total", "By")
var viewTotalAllocMem = &view.View{
	Name:        mRuntimeTotalAllocMem.Name(),
	Description: mRuntimeTotalAllocMem.Description(),
	Measure:     mRuntimeTotalAllocMem,
	Aggregation: view.Count(),
	TagKeys:     []tag.Key{},
}

var mRuntimeSysMem = stats.Int64("oc.io/process/sys_memory_alloc", "Number of bytes given to the process in total", "By")
var viewSysMem = &view.View{
	Name:        mRuntimeSysMem.Name(),
	Description: mRuntimeSysMem.Description(),
	Measure:     mRuntimeSysMem,
	Aggregation: view.LastValue(),
	TagKeys:     []tag.Key{},
}

var mCPUSeconds = stats.Int64("oc.io/process/cpu_seconds", "CPU seconds for this process", "1")
var viewCPUSeconds = &view.View{
	Name:        mCPUSeconds.Name(),
	Description: mCPUSeconds.Description(),
	Measure:     mCPUSeconds,
	Aggregation: view.Count(),
	TagKeys:     []tag.Key{},
}

// NewProcessMetricsViews creates a new set of ProcessMetrics (mem, cpu) that can be used to measure
// basic information about this process.
func NewProcessMetricsViews() *ProcessMetricsViews {
	return &ProcessMetricsViews{
		views: []*view.View{viewAllocMem, viewTotalAllocMem, viewSysMem, viewCPUSeconds},
		done:  make(chan struct{}),
	}
}

// StartCollection starts a ticker'd goroutine that will update the PMV measurements every 5 seconds
func (pmv *ProcessMetricsViews) StartCollection() {
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				pmv.updateViews()
			case <-pmv.done:
				return
			}
		}
	}()
}

// Views returns the views internal to the PMV
func (pmv *ProcessMetricsViews) Views() []*view.View {
	return pmv.views
}

// StopCollection stops the collection of the process metric information
func (pmv *ProcessMetricsViews) StopCollection() {
	close(pmv.done)
}

func (pmv *ProcessMetricsViews) updateViews() {
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
}

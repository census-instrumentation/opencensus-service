package internal

import (
	"context"
	"errors"
	"github.com/census-instrumentation/opencensus-service/consumer"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/storage"
	"go.uber.org/zap"
	"io"
	"sync"
	"sync/atomic"
)

const (
	runningStateInit = iota
	runningStateReady
	runningStateStop
)

var idSeq int64
var alreadyStopErr = errors.New("ocaStore already stopped")

// OpenCensus Store for prometheus
type ocaStore struct {
	running int32
	logger  *zap.SugaredLogger
	sink    consumer.MetricsConsumer
	mc      *mService
	once    *sync.Once
	ctx     context.Context
}

// ensure *ocaStore has implemented both scrap.Appendable and io.Closer
var _ scrape.Appendable = (*ocaStore)(nil)
var _ io.Closer = (*ocaStore)(nil)

func NewOcaStore(ctx context.Context, sink consumer.MetricsConsumer, logger *zap.SugaredLogger) *ocaStore {
	return &ocaStore{
		running: runningStateInit,
		ctx:     ctx,
		sink:    sink,
		logger:  logger,
		once:    &sync.Once{},
	}
}

func (o *ocaStore) SetScrapeManager(scrapeManager *scrape.Manager) {
	if scrapeManager != nil && atomic.CompareAndSwapInt32(&o.running, runningStateInit, runningStateReady) {
		o.mc = &mService{scrapeManager}
	}
}

func (o *ocaStore) Appender() (storage.Appender, error) {
	state := atomic.LoadInt32(&o.running)
	if state == runningStateReady {
		return newTransaction(o.ctx, o.mc, o.sink, o.logger), nil
	} else if state == runningStateInit {
		return nil, errors.New("ScrapeManager is not set")
	}
	// instead of returning an error, return a dummy appender instead, otherwise it can trigger panic
	return noopAppender(true), nil
}

func (o *ocaStore) Close() error {
	atomic.CompareAndSwapInt32(&o.running, runningStateReady, runningStateStop)
	return nil
}

// noop Appender, always return error on any operations

type noopAppender bool

func (noopAppender) Add(l labels.Labels, t int64, v float64) (uint64, error) {
	return 0, alreadyStopErr
}

func (noopAppender) AddFast(l labels.Labels, ref uint64, t int64, v float64) error {
	return alreadyStopErr
}

func (noopAppender) Commit() error {
	return alreadyStopErr
}

func (noopAppender) Rollback() error {
	return alreadyStopErr
}

var _ storage.Appender = noopAppender(true)

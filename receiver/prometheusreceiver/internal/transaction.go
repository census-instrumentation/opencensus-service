package internal

import (
	"context"
	"errors"
	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	"github.com/census-instrumentation/opencensus-service/consumer"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
	"go.uber.org/zap"
	"strings"
	"sync/atomic"
)

const (
	portAttr = "port"
	schemeAttr = "scheme"
)
var metricNameNotFoundErr = errors.New("metricName not found from labels")
var transactionAbortedErr = errors.New("transaction aborted")
var noJobInstanceErr = errors.New("job or instance cannot be found from labels")

// A transaction is corresponding to an individual scrape operation or stale report.
// That said, whenever prometheus receiver scrapped a target metric endpoint a page of raw metrics is returned,
// a transaction, which acts as appender, is created to process this page of data, the scrapeLoop will call the Add or
// AddFast method to insert metrics data points, when finished either Commit, which means success, is called and data
// will be flush to the downstream consumer, or Rollback, which means discard all the data, is called and all data
// points are discarded.
type transaction struct {
	id            int64
	ctx           context.Context
	isNew         bool
	sink          consumer.MetricsConsumer
	ms            MetadataService
	metricBuilder *metricBuilder
	logger  *zap.SugaredLogger
}

func newTransaction(ctx context.Context, ms MetadataService, sink consumer.MetricsConsumer, logger *zap.SugaredLogger) *transaction {
	return &transaction{
		id:    atomic.AddInt64(&idSeq, 1),
		ctx:   ctx,
		isNew: true,
		sink:  sink,
		ms:    ms,
		logger: logger,
	}
}

// ensure *transaction has implemented the storage.Appender interface
var _ storage.Appender = (*transaction)(nil)

// there's no document on the first return value, however, it's somehow used in AddFast. I assume this is like a
// uniqKey kind of thing for storage like a database, so that the operation can be perform faster with this key.
// however, in this case, return 0 like what the prometheus remote store does shall be enough
func (tr *transaction) Add(l labels.Labels, t int64, v float64) (uint64, error) {
	return 0, tr.AddFast(l, 0, t, v)
}

// returning an error from this method can cause the whole appending transaction to be aborted and fail
func (tr *transaction) AddFast(ls labels.Labels, _ uint64, t int64, v float64) error {
	select {
	case <-tr.ctx.Done():
		return transactionAbortedErr
	default:
	}
	if tr.isNew {
		if err := tr.initTransaction(ls); err != nil {
			return err
		}
	}
	return tr.metricBuilder.AddDataPoint(ls, t, v)
}

func (tr *transaction) initTransaction(ls labels.Labels) error {
	job, instance := ls.Get(model.JobLabel), ls.Get(model.InstanceLabel)
	if job == "" || instance == "" {
		return noJobInstanceErr
	}
	// discover the binding target when this method is called for the first time during a transaction
	mc, err := tr.ms.Get(job, instance)
	if err != nil {
		return err
	}
	node := createNode(job, instance, mc.SharedLabels().Get(model.SchemeLabel))
	tr.metricBuilder = NewMetricBuilder(node, mc, tr.logger)
	tr.isNew = false
	return nil
}

// submit metrics data to consumers
func (tr *transaction) Commit() error {
	if tr.isNew {
		// In a situation like not able to connect to the remote server, scrapeloop will still commit even if it had
		// never added any data points, that the transaction has not been initialized.
		return nil
	}
	if md, err := tr.metricBuilder.Build(); err == nil {
		if md == dummyMetric {
			return nil
		}
		return tr.sink.ConsumeMetricsData(context.Background(), *md)
	} else {
		return err
	}

}

func (tr *transaction) Rollback() error {
	return nil
}


func createNode(job, instance, scheme string) *commonpb.Node {
	splitted := strings.Split(instance, ":")
	host, port := splitted[0], "80"
	if len(splitted) >= 2 {
		port = splitted[1]
	}
	return &commonpb.Node{
		ServiceInfo: &commonpb.ServiceInfo{Name: job},
		Identifier: &commonpb.ProcessIdentifier{
			HostName: host,
		},
		Attributes: map[string]string{
			portAttr:    port,
			schemeAttr:  scheme,
		},
	}
}
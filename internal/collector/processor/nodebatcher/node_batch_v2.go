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

package nodebatcher

import (
	"context"
	"crypto/md5"
	"fmt"
	"math/rand"
	"sync"
	"time"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	resourcepb "github.com/census-instrumentation/opencensus-proto/gen-go/resource/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"github.com/golang/protobuf/proto"
	"go.opencensus.io/stats"
	"go.uber.org/zap"

	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/internal/collector/processor"
)

// batcherv2 is a component that accepts spans, and places them into batches grouped by node and resource.
//
// batcherv2 implements processor.SpanProcessor
//
// batcherv2 is a composition of four main pieces. First is its buckets map which maps nodes to buckets.
// Second is the nodebatcherv2 which keeps a batch associated with a single node, and sends it downstream.
// Third is a bucketTicker that ticks every so often and closes any open and not recently sent batches.
// Fourth is a batch which accepts spans and return when it should be closed due to size.
//
// When we no longer have to batch by node, the following changes should be made:
//   1) batcherv2 should be removed and nodebatcherv2 should be promoted to batcherv2
//   2) bucketTicker should be simplified significantly and replaced with a single ticker, since
//      tracking by node is no longer needed.
type batcherv2 struct {
	buckets sync.Map
	sender  processor.SpanProcessor
	tickers []*bucketTickerv2
	name    string
	logger  *zap.Logger

	removeAfterCycles uint32
	sendBatchSize     uint32
	numTickers        int
	tickTime          time.Duration
	timeout           time.Duration

	bucketMu sync.RWMutex
}

var _ processor.SpanProcessor = (*batcherv2)(nil)

// NewBatcherv2 creates a new batcherv2 that batches spans by node and resource
func NewBatcherv2(name string, logger *zap.Logger, sender processor.SpanProcessor) processor.SpanProcessor {
	// Init with defaults
	b := &batcherv2{
		name:   name,
		sender: sender,
		logger: logger,

		removeAfterCycles: defaultRemoveAfterCycles,
		sendBatchSize:     defaultSendBatchSize,
		numTickers:        defaultNumTickers,
		tickTime:          defaultTickTime,
		timeout:           defaultTimeout,
	}

	// start tickers after options loaded in
	b.tickers = newStartedBucketTickersForBatchv2(b)
	return b
}

// ProcessSpans implements batcherv2 as a SpanProcessor and takes the provided spans and adds them to
// batches
func (b *batcherv2) ProcessSpans(td data.TraceData, spanFormat string) error {
	bucketID := b.genBucketID(td.Node, td.Resource, spanFormat)
	bucket := b.getOrAddBucket(bucketID, td.Node, td.Resource, spanFormat)
	bucket.add(td.Spans)
	return nil
}

func (b *batcherv2) genBucketID(node *commonpb.Node, resource *resourcepb.Resource, spanFormat string) string {
	h := md5.New()
	if node != nil {
		nodeKey, err := proto.Marshal(node)
		if err != nil {
			b.logger.Error("Error marshalling node to batcherv2 mapkey.", zap.Error(err))
		} else {
			h.Write(nodeKey)
		}
	}
	if resource != nil {
		resourceKey, err := proto.Marshal(resource) // TODO: remove once resource is in span
		if err != nil {
			b.logger.Error("Error marshalling resource to batcherv2 mapkey.", zap.Error(err))
		} else {
			h.Write(resourceKey)
		}
	}
	return fmt.Sprintf("%x", h.Sum([]byte(spanFormat)))
}

func (b *batcherv2) getBucket(bucketID string) *nodeBatchV2 {
	bucket, ok := b.buckets.Load(bucketID)
	if ok {
		return bucket.(*nodeBatchV2)
	}
	return nil
}

func (b *batcherv2) getOrAddBucket(
	bucketID string, node *commonpb.Node, resource *resourcepb.Resource, spanFormat string,
) *nodeBatchV2 {
	bucket, alreadyStored := b.buckets.LoadOrStore(
		bucketID,
		newNodeBatch(b, spanFormat, node, resource),
	)
	// Add this bucket to a random ticker
	if !alreadyStored {
		stats.Record(context.Background(), statNodesAddedToBatches.M(1))
		b.tickers[rand.Intn(len(b.tickers))].add(bucketID)
	}
	return bucket.(*nodeBatchV2)
}

func (b *batcherv2) removeBucket(bucketID string) {
	stats.Record(context.Background(), statNodesRemovedFromBatches.M(1))
	b.buckets.Delete(bucketID)
}

type nodeBatchV2 struct {
	mu              sync.RWMutex
	items           []*tracepb.Span
	cyclesUntouched uint32
	dead            uint32

	parent   *batcherv2
	format   string
	node     *commonpb.Node
	resource *resourcepb.Resource
}

func newNodeBatch(
	parent *batcherv2,
	format string,
	node *commonpb.Node,
	resource *resourcepb.Resource,
) *nodeBatchV2 {
	return &nodeBatchV2{
		parent:   parent,
		format:   format,
		node:     node,
		resource: resource,
		items:    make([]*tracepb.Span, 0, initialBatchCapacity),
	}
}

func (nb *nodeBatchV2) add(spans []*tracepb.Span) {
	nb.mu.Lock()
	nb.items = append(nb.items, spans...)
	nb.cyclesUntouched = 0

	var itemsToProcess []*tracepb.Span
	if uint32(len(nb.items)) > nb.parent.sendBatchSize || nb.dead == nodeStatusDead {
		itemsToProcess = nb.items
		nb.items = make([]*tracepb.Span, 0, len(nb.items))
	}
	nb.mu.Unlock()

	if len(itemsToProcess) > 0 {
		td := data.TraceData{
			Node:     nb.node,
			Resource: nb.resource,
			Spans:    itemsToProcess,
		}

		_ = nb.parent.sender.ProcessSpans(td, nb.format)
	}
}

type bucketTickerv2 struct {
	ticker       *time.Ticker
	nodes        map[string]bool
	parent       *batcherv2
	pendingNodes chan string
	stopCn       chan struct{}
	once         sync.Once
}

func newStartedBucketTickersForBatchv2(b *batcherv2) []*bucketTickerv2 {
	tickers := make([]*bucketTickerv2, 0, b.numTickers)
	for ignored := 0; ignored < b.numTickers; ignored++ {
		ticker := newBucketTickerv2(b, b.tickTime)
		go ticker.start()
		tickers = append(tickers, ticker)
	}
	return tickers
}

func newBucketTickerv2(parent *batcherv2, tickTime time.Duration) *bucketTickerv2 {
	return &bucketTickerv2{
		ticker:       time.NewTicker(tickTime),
		nodes:        make(map[string]bool),
		parent:       parent,
		pendingNodes: make(chan string, tickerPendingNodesBuffer),
		stopCn:       make(chan struct{}),
	}
}

func (bt *bucketTickerv2) add(bucketID string) {
	bt.pendingNodes <- bucketID
}

func (bt *bucketTickerv2) start() {
	bt.once.Do(func() {
		for {
			select {
			case <-bt.ticker.C:
				for nbKey := range bt.nodes {
					nb := bt.parent.getBucket(nbKey)
					// Need to check nil here incase the node was deleted from the parent batcher, but
					// not deleted from bt.nodes
					nb.mu.Lock()
					if nb == nil {
						// re-delete just in case
						delete(bt.nodes, nbKey)
						nb.mu.Unlock()
						continue
					} else if len(nb.items) > 0 {
						// If the batch is non-empty, go ahead and cut it
						statsTags := processor.StatsTagsForBatch(
							nb.parent.name, processor.ServiceNameForNode(nb.node), nb.format,
						)
						_ = stats.RecordWithTags(context.Background(), statsTags, statTimeoutTriggerSend.M(1))
						itemsToProcess := nb.items
						nb.items = make([]*tracepb.Span, 0, initialBatchCapacity)
						td := data.TraceData{
							Node:     nb.node,
							Resource: nb.resource,
							Spans:    itemsToProcess,
						}

						_ = nb.parent.sender.ProcessSpans(td, nb.format)
					} else {
						if nb.cyclesUntouched > nb.parent.removeAfterCycles {
							nb.parent.removeBucket(nbKey)
							delete(bt.nodes, nbKey)
							nb.dead = nodeStatusDead
						}
					}
					nb.mu.Unlock()
				}
			case newBucketKey := <-bt.pendingNodes:
				bt.nodes[newBucketKey] = true
			case <-bt.stopCn:
				bt.ticker.Stop()
				return
			}
		}
	})
}

func (bt *bucketTickerv2) stop() {
	close(bt.stopCn)
}

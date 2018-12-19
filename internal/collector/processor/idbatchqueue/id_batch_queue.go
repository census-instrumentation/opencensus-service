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

// Package idbatchqueue defines a FIFO queue of fixed size in which the
// elements are batches of ids defined as byte slices.
package idbatchqueue

import (
	"errors"
	"sync"
)

var (
	// ErrInvalidNumBatches occurs when an invalid number of batches is specified.
	ErrInvalidNumBatches = errors.New("invalid number of batches, it must be greater than zero")
	// ErrInvalidBatchChannelSize occurs when an invalid batch channel size is specified.
	ErrInvalidBatchChannelSize = errors.New("invalid batch channel size, it must be greater than zero")
)

// ID is the type of each element in the batch.
type ID []byte

// Batch is the type of batches held by the Batcher.
type Batch []ID

// Batcher behaves like a FIFO queue of a fixed number of batches, in which
// items can be enqueued concurrently. The caller is in control of when a batch is
// completed and a new one should be started.
type Batcher interface {
	// Enqueue puts the given id on the batch being currently built. The client is in charge
	// of limiting the growth of the current batch if appropriate for its scenario. It can
	// either call Dequeue earlier or stop adding new items depending on what is required by
	// the scenario.
	Enqueue(id ID)
	// Dequeue takes the batch at the front of the queue, and moves the current batch to
	// the end of the queue, creating a new batch to receive new enqueued items.
	// It returns the batch that was in front of the queue and a boolean
	// that if true indicates that there are more batches to be retrieved.
	Dequeue() (Batch, bool)
	// Stop informs that no more items are going to be enqueued. After
	// this method is called attempts to enqueue new items will panic.
	Stop()
}

var _ Batcher = (*batcher)(nil)

type batcher struct {
	pendingIds chan ID    // Channel for the ids to be added to the next batch.
	batches    chan Batch // Channel with already captured batches.

	// cbMutex protects the currentBatch storing ids.
	cbMutex      sync.Mutex
	currentBatch Batch

	numBatches                uint64
	newBatchesInitialCapacity uint64
	stopchan                  chan bool
	stopped                   bool
}

// New creates a Batcher that will hold numBatches in its FIFO queue, having a channel with
// batchChannelSize to receive new items. New batches will be created with capacity set to
// newBatchesInitialCapacity.
func New(numBatches, newBatchesInitialCapacity, batchChannelSize uint64) (Batcher, error) {
	if numBatches < 1 {
		return nil, ErrInvalidNumBatches
	}
	if batchChannelSize < 1 {
		return nil, ErrInvalidBatchChannelSize
	}

	batches := make(chan Batch, numBatches)
	// First numBatches batches will be empty in order to simplify clients that are running
	// Dequeue on a timer and want to delay the processing of the first batch with actual
	// data. This way there is no need for accounting on the client side and single timer
	// can be started immediately.
	for i := uint64(0); i < numBatches; i++ {
		batches <- nil
	}

	batcher := &batcher{
		pendingIds:                make(chan ID, batchChannelSize),
		batches:                   batches,
		currentBatch:              make(Batch, 0, newBatchesInitialCapacity),
		newBatchesInitialCapacity: newBatchesInitialCapacity,
		stopchan:                  make(chan bool),
	}

	// Single goroutine that keeps filling the current batch, contention is expected only
	// when the current batch is being switched.
	go func() {
		for id := range batcher.pendingIds {
			batcher.cbMutex.Lock()
			batcher.currentBatch = append(batcher.currentBatch, id)
			batcher.cbMutex.Unlock()
		}
		batcher.stopchan <- true
	}()

	return batcher, nil
}

// Enqueue puts the given id on the batch being currently built. The client is in charge
// of limiting the growth of the current batch if appropriate for its scenario. It can
// either call Dequeue earlier or stop adding new items depending on what is required by
// the scenario.
func (b *batcher) Enqueue(id ID) {
	b.pendingIds <- id
}

// Dequeue takes the batch at the front of the queue, and moves the current batch to
// the end of the queue, creating a new batch to receive new enqueued items.
// It returns the batch that was in front of the queue and a boolean
// that if true indicates that there are more batches to be retrieved.
func (b *batcher) Dequeue() (Batch, bool) {
	for readBatch := range b.batches {
		if !b.stopped {
			nextBatch := make(Batch, 0, b.newBatchesInitialCapacity)
			b.cbMutex.Lock()
			b.batches <- b.currentBatch
			b.currentBatch = nextBatch
			b.cbMutex.Unlock()
		}
		return readBatch, true
	}

	readBatch := b.currentBatch
	b.currentBatch = nil
	return readBatch, false
}

// Stop informs that no more items are going to be enqueued. After
// this method is called attempts to enqueue new items will panic.
func (b *batcher) Stop() {
	close(b.pendingIds)
	b.stopped = <-b.stopchan
	close(b.batches)
}

package unflushed

import (
	"fmt"
	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// UnflushedDataCache serves reads on data that has not yet been fully flushed to disk. Data is
// inserted via PutBatch and remains visible to Get until it has been reported as durable by both
// ReportFlushedKeys (keymap) and ReportFlushedSegment (segment on disk). A background goroutine
// compares the two reporting streams and evicts entries once both conditions are met.
type UnflushedDataCache struct {
	logger       *slog.Logger
	errorMonitor *util.ErrorMonitor

	// Thread-safe map of key string → value bytes.
	cache *util.SyncMap[string, []byte]

	// Work for the background eviction loop.
	workChan chan any

	// Key strings that have been reported as durable in the keymap, in write order.
	flushedKeys *util.RandomAccessDeque[string]

	// Key strings whose segment data has been reported as durable on disk, in write order.
	flushedSegments *util.RandomAccessDeque[string]

	// Set to true when the loop should stop after the current iteration.
	stopped bool
}

// NewUnflushedDataCache creates a new UnflushedDataCache and starts its background eviction goroutine.
func NewUnflushedDataCache(
	logger *slog.Logger,
	errorMonitor *util.ErrorMonitor,
	workChanSize int,
) *UnflushedDataCache {
	u := &UnflushedDataCache{
		logger:          logger,
		errorMonitor:    errorMonitor,
		cache:           util.NewSyncMap[string, []byte](),
		workChan:        make(chan any, workChanSize),
		flushedKeys:     util.NewRandomAccessDeque[string](64),
		flushedSegments: util.NewRandomAccessDeque[string](64),
	}
	go u.run()
	return u
}

// Get returns the cached value for a key, or nil if the key is not in the cache.
func (u *UnflushedDataCache) Get(key []byte) ([]byte, bool) {
	return u.cache.Get(util.UnsafeBytesToString(key))
}

// PutBatch inserts all primary and secondary keys from the batch into the cache. After this
// method returns, data is visible to Get and will remain so until both reporting methods have
// confirmed durability.
func (u *UnflushedDataCache) PutBatch(batch []*types.PutRequest) {
	entries := make(map[string][]byte, len(batch))
	for _, req := range batch {
		entries[util.UnsafeBytesToString(req.Key)] = req.Value
		for _, sk := range req.SecondaryKeys {
			subrange := req.Value[sk.Offset : sk.Offset+sk.Length]
			entries[util.UnsafeBytesToString(sk.Key)] = subrange
		}
	}
	u.cache.PutBatch(entries)
}

// ReportFlushedKeys reports keys that are now durable in the keymap. Keys must be reported in
// the same order they were originally written via PutBatch.
func (u *UnflushedDataCache) ReportFlushedKeys(keys []*types.ScopedKey) error {
	err := util.Send(u.errorMonitor, u.workChan, &reportFlushedKeysMsg{keys: keys})
	if err != nil {
		return fmt.Errorf("failed to enqueue flushed keys report: %w", err)
	}
	return nil
}

// ReportFlushedSegment reports keys whose segment data is now durable on disk. Keys must be
// reported in the same order they were originally written via PutBatch.
func (u *UnflushedDataCache) ReportFlushedSegment(keys []*types.ScopedKey) error {
	err := util.Send(u.errorMonitor, u.workChan, &reportFlushedSegmentMsg{keys: keys})
	if err != nil {
		return fmt.Errorf("failed to enqueue flushed segment report: %w", err)
	}
	return nil
}

// Stop cleanly shuts down the background eviction goroutine. Blocks until shutdown is complete.
func (u *UnflushedDataCache) Stop() error {
	responseChan := make(chan struct{}, 1)
	err := util.Send(u.errorMonitor, u.workChan, &shutdownMsg{responseChan: responseChan})
	if err != nil {
		return fmt.Errorf("failed to enqueue shutdown request: %w", err)
	}
	_, err = util.Await(u.errorMonitor, responseChan)
	if err != nil {
		return fmt.Errorf("failed to wait for shutdown completion: %w", err)
	}
	return nil
}

// run is the background eviction loop. It receives flushed-key and flushed-segment reports,
// populates the two deques, and evicts cache entries that appear at the front of both deques.
// Because both reporting streams deliver keys in the original write order, the deque fronts
// always correspond to the same logical key when both are non-empty.
func (u *UnflushedDataCache) run() {
	for !u.stopped {
		select {
		case <-u.errorMonitor.ImmediateShutdownRequired():
			u.logger.Info("shutting down unflushed data cache due to error monitor")
			return
		case message := <-u.workChan:
			if msg, ok := message.(*reportFlushedKeysMsg); ok {
				for _, k := range msg.keys {
					u.flushedKeys.PushBack(util.UnsafeBytesToString(k.Key))
				}
			} else if msg, ok := message.(*reportFlushedSegmentMsg); ok {
				for _, k := range msg.keys {
					u.flushedSegments.PushBack(util.UnsafeBytesToString(k.Key))
				}
			} else if msg, ok := message.(*shutdownMsg); ok {
				msg.responseChan <- struct{}{}
				u.stopped = true
			} else {
				u.errorMonitor.Panic(fmt.Errorf("unknown unflushed data cache message type %T", message))
				u.stopped = true
			}

			u.tryEvict()
		}
	}
}

// tryEvict drains matching entries from the fronts of both deques and batches them into a single
// cache delete to minimize mutex acquisitions.
func (u *UnflushedDataCache) tryEvict() {
	var toDelete []string
	for !u.flushedKeys.IsEmpty() && !u.flushedSegments.IsEmpty() {
		keyFromKeymap := u.flushedKeys.PeekFront()
		keyFromSegment := u.flushedSegments.PeekFront()

		if keyFromKeymap != keyFromSegment {
			break
		}

		u.flushedKeys.PopFront()
		u.flushedSegments.PopFront()
		toDelete = append(toDelete, keyFromKeymap)
	}
	if len(toDelete) > 0 {
		u.cache.DeleteBatch(toDelete)
	}
}

// Messages sent to the background eviction loop.

type reportFlushedKeysMsg struct {
	keys []*types.ScopedKey
}

type reportFlushedSegmentMsg struct {
	keys []*types.ScopedKey
}

type shutdownMsg struct {
	responseChan chan struct{}
}

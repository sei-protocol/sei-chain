package unflushed

import (
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/segment"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// Responsible for serving reads on data that is not fully flushed to disk. Once data is flushed to disk it can be
// served from disk. But before then, attempting to read it from disk is unsafe.
type UnflushedDataCache struct {
	cache *util.SyncMap[string, []byte]

	// Keys that have been flushed to the keymap.
	flushedKeys util.RandomAccessDeque[*segment.Segment]

	// Keys from segments that have been flushed to disk.
	flushedSegments util.RandomAccessDeque[*segment.Segment]
}

// Create a new unflushed data cache and start its background goroutine.
func NewUnflushedDataCache() *UnflushedDataCache {
	return &UnflushedDataCache{}
}

// Get a value from the unflushed data cache.
func (u *UnflushedDataCache) Get(key []byte) ([]byte, error) {
	// TODO: get from cache immediately
	return nil, nil
}

// Put a batch of keys into the unflushed data cache. After this method returns, provided data will
// be visiable to Get(), and will remain visible until ReportFlushedKeys() and ReportFlushedSegment() has been
// called for the keys in the batch.
func (u *UnflushedDataCache) PutBatch(batch []*types.PutRequest) error {
	// TODO insert into cache immediately
	return nil
}

// Report a batch of keys that are now durable in the keymap. Keys should be passed to this method in the order
// they were passed to PutBatch().
func (u *UnflushedDataCache) ReportFlushedKeys(keys []*types.ScopedKey) error {
	// TODO: send batch of keys to background worker
	return nil
}

// Report a segment that is now durable in the disk table. Keys should be passed to this method in the order
// they were passed to PutBatch().
func (u *UnflushedDataCache) ReportFlushedSegment(segment *segment.Segment) error {
	// TODO: send segment to background worker
	return nil
}

func (u *UnflushedDataCache) run() {
    // TODO: pop from work queue and populate flushedKeys and flushedSegments. Pop values from those queues and compare,
	// elements are eligible for eviction from the cache when the key appears in both queues. You can assume
	// 
}

// TODO add a shutdown workflow
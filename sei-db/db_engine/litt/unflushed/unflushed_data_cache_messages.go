package unflushed

import "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"

// Sent to the unflushed data cache control loop when keys are flushed
// (i.e. when UnflushedDataCache.ReportFlushedKeys is called).
type reportFlushedKeysMsg struct {
	keys []types.ScopedKey
}

// Sent to the unflushed data cache control loop when segment data is flushed
// (i.e. when UnflushedDataCache.ReportFlushedSegment is called).
type reportFlushedSegmentMsg struct {
	keys []types.ScopedKey
}

// Sent to the unflushed data cache control loop when the cache should be shut down.
type shutdownMsg struct {
	responseChan chan struct{}
}

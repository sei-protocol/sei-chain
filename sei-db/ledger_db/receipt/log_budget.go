package receipt

import (
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

const (
	// DefaultMaxLogBytes is the default total estimated heap bytes a single
	// eth_getLogs query may materialize in its final result before it errors.
	DefaultMaxLogBytes = 64 << 20 // 64 MiB

	// Per-log heap overhead beyond Address/Data/Topics payload bytes: the slice
	// slot pointer, struct fields, and topics slice header. This bounds peak
	// heap for OOM prevention, not JSON-RPC wire size.
	logHeapPointerOverhead = int64(8)
	logHeapStructOverhead  = int64(64)
	logTopicsSliceHeader   = int64(24)
)

// LogBudget tracks matched-log count and estimated heap bytes for eth_getLogs
// queries. Call Reserve before appending each log; Reserve returns an error
// without charging the budget when either ceiling would be exceeded (> limit,
// not >=). Concurrent Reserve calls may overshoot by ~O(workers) before Tripped
// is observed.
type LogBudget struct {
	mu        sync.Mutex
	usedBytes int64
	usedCount int64
	maxBytes  int64
	maxLog    int64
	tripped   atomic.Bool
	tripErr   error
}

func NewLogBudget(maxLog, maxBytes int64) *LogBudget {
	return &LogBudget{maxLog: maxLog, maxBytes: maxBytes}
}

// NewLogBudgetBytesOnly caps estimated heap bytes without a matched-log count
// ceiling. Used by the litt store on the range-query path for early OOM abort
// before eth normalization rebuilds canonical logs.
func NewLogBudgetBytesOnly(maxBytes int64) *LogBudget {
	return NewLogBudget(0, maxBytes)
}

// EstimateLogHeapBytes approximates the heap footprint of retaining one log in
// a result slice.
func EstimateLogHeapBytes(log *ethtypes.Log) int64 {
	if log == nil {
		return 0
	}
	return int64(common.AddressLength) +
		int64(len(log.Data)) +
		logTopicsSliceHeader +
		int64(common.HashLength*len(log.Topics)) +
		logHeapPointerOverhead +
		logHeapStructOverhead
}

// Reserve charges the budget for log without mutating caller state. Returns an
// error when maxLog or maxBytes would be exceeded; a non-positive limit disables
// that dimension.
func (b *LogBudget) Reserve(log *ethtypes.Log) error {
	if b == nil {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.tripErr != nil {
		return b.tripErr
	}

	logBytes := EstimateLogHeapBytes(log)
	newCount := b.usedCount + 1
	newBytes := b.usedBytes + logBytes

	if b.maxLog > 0 && newCount > b.maxLog {
		b.tripErr = NewTooManyLogsError(b.maxLog)
		b.tripped.Store(true)
		return b.tripErr
	}
	if b.maxBytes > 0 && newBytes > b.maxBytes {
		b.tripErr = NewTooManyLogBytesError(b.maxBytes)
		b.tripped.Store(true)
		return b.tripErr
	}

	b.usedCount = newCount
	b.usedBytes = newBytes
	return nil
}

func (b *LogBudget) Tripped() bool {
	if b == nil {
		return false
	}
	return b.tripped.Load()
}

func (b *LogBudget) Err() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tripErr
}

func (b *LogBudget) UsedCount() int64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.usedCount
}

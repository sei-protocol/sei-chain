package keeper

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/cockroachdb/pebble/v2"
	"github.com/ethereum/go-ethereum/common"
)

// TraceCache stores pre-computed debug_trace results in a dedicated pebble db
// so writes don't share LSM with the chain state. Key shape:
//
//	"ts/" || height(BE,8) || tracerLen(1) || tracer || txHash(32)
//
// Tx hashes are globally unique on this chain, so (height, tracer, txHash) is
// sufficient. height is leading so a single range delete prunes a window.
type TraceCache struct {
	db *pebble.DB

	enqMu    sync.Mutex
	enqueuer TraceEnqueuer
}

const traceCachePrefix = "ts/"

// NewTraceCache opens (or creates) the trace cache pebble db at
// <homeDir>/data/trace_cache.
func NewTraceCache(homeDir string) (*TraceCache, error) {
	dir := filepath.Join(homeDir, "data", "trace_cache")
	db, err := pebble.Open(dir, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("open trace cache: %w", err)
	}
	return &TraceCache{db: db}, nil
}

func (c *TraceCache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

func traceCacheKey(height int64, tracer string, txHash common.Hash) []byte {
	if len(tracer) > 255 {
		tracer = tracer[:255]
	}
	out := make([]byte, 0, len(traceCachePrefix)+8+1+len(tracer)+32)
	out = append(out, traceCachePrefix...)
	var hb [8]byte
	binary.BigEndian.PutUint64(hb[:], uint64(height))
	out = append(out, hb[:]...)
	out = append(out, byte(len(tracer)))
	out = append(out, tracer...)
	out = append(out, txHash[:]...)
	return out
}

// Put stores a trace result. Safe to call on a nil receiver (no-op).
func (c *TraceCache) Put(height int64, tracer string, txHash common.Hash, value json.RawMessage) error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Set(traceCacheKey(height, tracer, txHash), value, pebble.NoSync)
}

// Get returns the cached trace, or (nil, false, nil) on miss. Safe on a nil
// receiver (returns miss).
func (c *TraceCache) Get(height int64, tracer string, txHash common.Hash) (json.RawMessage, bool, error) {
	if c == nil || c.db == nil {
		return nil, false, nil
	}
	val, closer, err := c.db.Get(traceCacheKey(height, tracer, txHash))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("trace cache get: %w", err)
	}
	out := make(json.RawMessage, len(val))
	copy(out, val)
	_ = closer.Close()
	return out, true, nil
}

// Prune deletes cache entries with height strictly less than belowHeight.
// Implemented as a single pebble range delete on the height-prefixed keyspace.
func (c *TraceCache) Prune(belowHeight int64) error {
	if c == nil || c.db == nil || belowHeight <= 0 {
		return nil
	}
	var lo, hi [8]byte
	binary.BigEndian.PutUint64(lo[:], 0)
	binary.BigEndian.PutUint64(hi[:], uint64(belowHeight))
	start := append([]byte(traceCachePrefix), lo[:]...)
	end := append([]byte(traceCachePrefix), hi[:]...)
	return c.db.DeleteRange(start, end, pebble.NoSync)
}

// TraceEnqueuer is implemented by the trace baker; the keeper holds a
// reference (via SetTraceEnqueuer) and forwards block heights to it from
// EndBlock so the baker can re-execute off the consensus path.
type TraceEnqueuer interface {
	Enqueue(height int64)
}

// SetTraceEnqueuer wires a TraceEnqueuer onto the cache so the keeper has a
// single field that owns both. Safe to call multiple times; nil disables.
func (c *TraceCache) SetTraceEnqueuer(e TraceEnqueuer) {
	if c == nil {
		return
	}
	c.enqMu.Lock()
	defer c.enqMu.Unlock()
	c.enqueuer = e
}

// Enqueue forwards a height to the registered enqueuer if any. Non-blocking
// by contract of the enqueuer; safe on a nil cache.
func (c *TraceCache) Enqueue(height int64) {
	if c == nil {
		return
	}
	c.enqMu.Lock()
	e := c.enqueuer
	c.enqMu.Unlock()
	if e != nil {
		e.Enqueue(height)
	}
}

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

const (
	traceCachePrefix      = "ts/" // per-tx: ts/<height,8>/<tracerLen,1><tracer>/<txHash,32>
	traceCacheBlockPrefix = "tb/" // per-block: tb/<height,8>/<tracerLen,1><tracer>
	traceCacheLastBakedKy = "meta/last_baked_height"
)

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
	binary.BigEndian.PutUint64(hb[:], uint64(height)) //nolint:gosec // block heights are non-negative
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

// traceCacheBlockKey builds the per-block key. Same height/tracer encoding
// as the per-tx key, just a different prefix and no txHash suffix.
func traceCacheBlockKey(height int64, tracer string) []byte {
	if len(tracer) > 255 {
		tracer = tracer[:255]
	}
	out := make([]byte, 0, len(traceCacheBlockPrefix)+8+1+len(tracer))
	out = append(out, traceCacheBlockPrefix...)
	var hb [8]byte
	binary.BigEndian.PutUint64(hb[:], uint64(height)) //nolint:gosec // block heights are non-negative
	out = append(out, hb[:]...)
	out = append(out, byte(len(tracer)))
	out = append(out, tracer...)
	return out
}

// PutBlock stores the assembled per-block trace result (JSON-marshaled
// []*TxTraceResult) so block-level reads are a single PK seek instead of N
// per-tx seeks. Safe on a nil receiver.
func (c *TraceCache) PutBlock(height int64, tracer string, value json.RawMessage) error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Set(traceCacheBlockKey(height, tracer), value, pebble.NoSync)
}

// GetBlock returns the cached per-block result, or (nil, false, nil) on miss.
// Safe on a nil receiver.
func (c *TraceCache) GetBlock(height int64, tracer string) (json.RawMessage, bool, error) {
	if c == nil || c.db == nil {
		return nil, false, nil
	}
	val, closer, err := c.db.Get(traceCacheBlockKey(height, tracer))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("trace cache get block: %w", err)
	}
	out := make(json.RawMessage, len(val))
	copy(out, val)
	_ = closer.Close()
	return out, true, nil
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

// SetLastBakedHeight records the highest block height the baker has fully
// processed. Only writes when h is strictly greater than the stored value
// (atomic max under a small lock) so out-of-order workers can't roll it
// back. Safe on a nil receiver.
func (c *TraceCache) SetLastBakedHeight(h int64) error {
	if c == nil || c.db == nil {
		return nil
	}
	c.enqMu.Lock()
	defer c.enqMu.Unlock()
	cur, err := c.lastBakedHeightUnlocked()
	if err != nil {
		return err
	}
	if h <= cur {
		return nil
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(h)) //nolint:gosec
	return c.db.Set([]byte(traceCacheLastBakedKy), b[:], pebble.NoSync)
}

// LastBakedHeight returns the highest block height the baker has recorded as
// fully processed, or 0 if unset. Safe on a nil receiver.
func (c *TraceCache) LastBakedHeight() (int64, error) {
	if c == nil || c.db == nil {
		return 0, nil
	}
	c.enqMu.Lock()
	defer c.enqMu.Unlock()
	return c.lastBakedHeightUnlocked()
}

func (c *TraceCache) lastBakedHeightUnlocked() (int64, error) {
	val, closer, err := c.db.Get([]byte(traceCacheLastBakedKy))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return 0, nil
		}
		return 0, fmt.Errorf("read last_baked_height: %w", err)
	}
	defer func() { _ = closer.Close() }()
	if len(val) != 8 {
		return 0, fmt.Errorf("trace cache: invalid last_baked_height length %d", len(val))
	}
	return int64(binary.BigEndian.Uint64(val)), nil //nolint:gosec
}

// Prune deletes per-tx and per-block cache entries with height strictly less
// than belowHeight. Two pebble range deletes — one per prefix — both bounded
// work regardless of how many rows are below.
func (c *TraceCache) Prune(belowHeight int64) error {
	if c == nil || c.db == nil || belowHeight <= 0 {
		return nil
	}
	var lo, hi [8]byte
	binary.BigEndian.PutUint64(lo[:], 0)
	binary.BigEndian.PutUint64(hi[:], uint64(belowHeight)) //nolint:gosec // block heights are non-negative
	for _, prefix := range []string{traceCachePrefix, traceCacheBlockPrefix} {
		start := append([]byte(prefix), lo[:]...)
		end := append([]byte(prefix), hi[:]...)
		if err := c.db.DeleteRange(start, end, pebble.NoSync); err != nil {
			return err
		}
	}
	return nil
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

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

// TraceDB stores pre-computed debug_trace results in a pebble db at
// <home>/data/trace_db. Two keyspaces, both height-leading so one range
// delete per prefix prunes a window:
//
//	ts/<height,8>/<tracerLen,1><tracer>/<txHash,32>   per-tx
//	tb/<height,8>/<tracerLen,1><tracer>               per-block (pre-encoded array)
type TraceDB struct {
	db *pebble.DB

	enqMu    sync.Mutex
	enqueuer TraceEnqueuer
}

const (
	traceDBPrefix       = "ts/"
	traceDBBlockPrefix  = "tb/"
	traceDBLastBakedKey = "meta/last_baked_height"
)

func NewTraceDB(homeDir string) (*TraceDB, error) {
	dir := filepath.Join(homeDir, "data", "trace_db")
	db, err := pebble.Open(dir, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("open trace db: %w", err)
	}
	return &TraceDB{db: db}, nil
}

func (c *TraceDB) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	// Drain the baker before closing pebble so workers don't write to a closed db.
	c.enqMu.Lock()
	e := c.enqueuer
	c.enqueuer = nil
	c.enqMu.Unlock()
	if e != nil {
		e.Stop()
	}
	return c.db.Close()
}

func traceDBKey(height int64, tracer string, txHash common.Hash) []byte {
	if len(tracer) > 255 {
		tracer = tracer[:255]
	}
	out := make([]byte, 0, len(traceDBPrefix)+8+1+len(tracer)+32)
	out = append(out, traceDBPrefix...)
	var hb [8]byte
	binary.BigEndian.PutUint64(hb[:], uint64(height)) //nolint:gosec
	out = append(out, hb[:]...)
	out = append(out, byte(len(tracer)))
	out = append(out, tracer...)
	out = append(out, txHash[:]...)
	return out
}

func traceDBBlockKey(height int64, tracer string) []byte {
	if len(tracer) > 255 {
		tracer = tracer[:255]
	}
	out := make([]byte, 0, len(traceDBBlockPrefix)+8+1+len(tracer))
	out = append(out, traceDBBlockPrefix...)
	var hb [8]byte
	binary.BigEndian.PutUint64(hb[:], uint64(height)) //nolint:gosec
	out = append(out, hb[:]...)
	out = append(out, byte(len(tracer)))
	out = append(out, tracer...)
	return out
}

func (c *TraceDB) Put(height int64, tracer string, txHash common.Hash, value json.RawMessage) error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Set(traceDBKey(height, tracer, txHash), value, pebble.NoSync)
}

func (c *TraceDB) Get(height int64, tracer string, txHash common.Hash) (json.RawMessage, bool, error) {
	if c == nil || c.db == nil {
		return nil, false, nil
	}
	val, closer, err := c.db.Get(traceDBKey(height, tracer, txHash))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("trace db get: %w", err)
	}
	out := make(json.RawMessage, len(val))
	copy(out, val)
	_ = closer.Close()
	return out, true, nil
}

func (c *TraceDB) PutBlock(height int64, tracer string, value json.RawMessage) error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Set(traceDBBlockKey(height, tracer), value, pebble.NoSync)
}

func (c *TraceDB) GetBlock(height int64, tracer string) (json.RawMessage, bool, error) {
	if c == nil || c.db == nil {
		return nil, false, nil
	}
	val, closer, err := c.db.Get(traceDBBlockKey(height, tracer))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("trace db get block: %w", err)
	}
	out := make(json.RawMessage, len(val))
	copy(out, val)
	_ = closer.Close()
	return out, true, nil
}

// SetLastBakedHeight records the highest fully-processed block. Atomic max:
// out-of-order workers can't roll it back.
func (c *TraceDB) SetLastBakedHeight(h int64) error {
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
	return c.db.Set([]byte(traceDBLastBakedKey), b[:], pebble.NoSync)
}

func (c *TraceDB) LastBakedHeight() (int64, error) {
	if c == nil || c.db == nil {
		return 0, nil
	}
	c.enqMu.Lock()
	defer c.enqMu.Unlock()
	return c.lastBakedHeightUnlocked()
}

func (c *TraceDB) lastBakedHeightUnlocked() (int64, error) {
	val, closer, err := c.db.Get([]byte(traceDBLastBakedKey))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return 0, nil
		}
		return 0, fmt.Errorf("read last_baked_height: %w", err)
	}
	defer func() { _ = closer.Close() }()
	if len(val) != 8 {
		return 0, fmt.Errorf("trace db: invalid last_baked_height length %d", len(val))
	}
	return int64(binary.BigEndian.Uint64(val)), nil //nolint:gosec
}

// Prune deletes per-tx and per-block rows with height < belowHeight.
func (c *TraceDB) Prune(belowHeight int64) error {
	if c == nil || c.db == nil || belowHeight <= 0 {
		return nil
	}
	var lo, hi [8]byte
	binary.BigEndian.PutUint64(lo[:], 0)
	binary.BigEndian.PutUint64(hi[:], uint64(belowHeight)) //nolint:gosec
	for _, prefix := range []string{traceDBPrefix, traceDBBlockPrefix} {
		start := append([]byte(prefix), lo[:]...)
		end := append([]byte(prefix), hi[:]...)
		if err := c.db.DeleteRange(start, end, pebble.NoSync); err != nil {
			return err
		}
	}
	return nil
}

// TraceEnqueuer is implemented by the trace baker.
type TraceEnqueuer interface {
	Enqueue(height int64)
	Stop()
}

func (c *TraceDB) SetTraceEnqueuer(e TraceEnqueuer) {
	if c == nil {
		return
	}
	c.enqMu.Lock()
	defer c.enqMu.Unlock()
	c.enqueuer = e
}

func (c *TraceDB) Enqueue(height int64) {
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

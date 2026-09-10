package walsim

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

// walStore is the minimal WAL surface the benchmark drives. The new seiwal WAL (seiwal.WAL[[]byte])
// satisfies it directly with no wrapper; the legacy sei-db/wal WAL is adapted by legacyWALShim.
type walStore interface {
	// Append schedules a record with the given index and payload.
	Append(index uint64, data []byte) error

	// Flush blocks until previously scheduled appends are durable.
	Flush() error

	// Bounds reports whether any record is stored and, if so, the lowest and highest stored indices.
	Bounds() (ok bool, first uint64, last uint64, err error)

	// PruneBefore requests removal of all records with an index below lowestIndexToKeep.
	PruneBefore(lowestIndexToKeep uint64) error

	// Close flushes pending appends and releases resources.
	Close() error
}

// The seiwal WAL implements walStore directly.
var _ walStore = (seiwal.WAL[[]byte])(nil)

// The legacy shim implements walStore.
var _ walStore = (*legacyWALShim)(nil)

// openWALStore opens the WAL backend selected by the configuration.
func openWALStore(ctx context.Context, config *WalsimConfig) (walStore, error) {
	switch config.Backend {
	case "seiwal":
		cfg := config.Seiwal
		// walsim owns the storage path and metric name.
		cfg.Path = config.DataDir
		cfg.Name = "walsim"
		w, err := seiwal.NewWAL(&cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to open seiwal WAL: %w", err)
		}
		return w, nil
	case "legacy":
		return newLegacyWALShim(ctx, config)
	default:
		return nil, fmt.Errorf("unknown WAL backend: %q", config.Backend)
	}
}

// legacyWALShim adapts the legacy sei-db/wal WAL to the walStore interface so the benchmark can
// drive it exactly like the new seiwal WAL. It is throwaway code: the legacy WAL is scheduled for
// deletion, and this shim goes with it.
//
// The legacy WAL's TruncateBefore rewrites segment files and is O(segments), so forwarding it on
// every request (as the size-target prune loop wants) would be catastrophic. The shim therefore
// coalesces prune requests, forwarding only every pruneRelaxationFactor-th PruneBefore to the
// underlying TruncateBefore.
type legacyWALShim struct {
	inner *wal.WAL[[]byte]

	// Forward a TruncateBefore only once per this many PruneBefore calls.
	pruneRelaxationFactor uint64

	// The number of PruneBefore calls received so far.
	pruneRequestCount uint64
}

func newLegacyWALShim(ctx context.Context, config *WalsimConfig) (*legacyWALShim, error) {
	identity := func(b []byte) ([]byte, error) { return b, nil }

	cfg := config.Legacy
	// Force the legacy WAL's own background pruning off so pruning is driven solely through
	// PruneBefore (and coalesced below).
	cfg.KeepRecent = 0
	cfg.PruneInterval = 0

	inner, err := wal.NewWAL[[]byte](ctx, identity, identity, config.DataDir, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open legacy WAL: %w", err)
	}

	return &legacyWALShim{
		inner:                 inner,
		pruneRelaxationFactor: config.PruneRelaxationFactor,
	}, nil
}

// Append writes data to the legacy WAL. The legacy WAL assigns its own 1-based, contiguous index,
// so the caller-supplied index is ignored; walsim seeds its counter from Bounds, keeping the two
// aligned.
func (s *legacyWALShim) Append(_ uint64, data []byte) error {
	if err := s.inner.Write(data); err != nil {
		return fmt.Errorf("failed to write to legacy WAL: %w", err)
	}
	return nil
}

// Flush is a no-op. The legacy WAL exposes no explicit sync, so durability is governed entirely by
// its FsyncEnabled and batching configuration.
func (s *legacyWALShim) Flush() error {
	return nil
}

// Bounds reports the stored index range. The legacy WAL reports 0/0 for an empty log; real indices
// are 1-based, so a last offset of 0 means empty.
func (s *legacyWALShim) Bounds() (bool, uint64, uint64, error) {
	first, err := s.inner.FirstOffset()
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to read first offset: %w", err)
	}
	last, err := s.inner.LastOffset()
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to read last offset: %w", err)
	}
	if last == 0 {
		return false, 0, 0, nil
	}
	return true, first, last, nil
}

// PruneBefore coalesces prune requests: only every pruneRelaxationFactor-th call reaches the
// underlying TruncateBefore, which is expensive in the legacy WAL. The most recent
// lowestIndexToKeep is used when the forward fires.
func (s *legacyWALShim) PruneBefore(lowestIndexToKeep uint64) error {
	s.pruneRequestCount++
	if s.pruneRequestCount%s.pruneRelaxationFactor != 0 {
		return nil
	}
	if err := s.inner.TruncateBefore(lowestIndexToKeep); err != nil {
		return fmt.Errorf("failed to truncate legacy WAL: %w", err)
	}
	return nil
}

// Close shuts down the legacy WAL.
func (s *legacyWALShim) Close() error {
	if err := s.inner.Close(); err != nil {
		return fmt.Errorf("failed to close legacy WAL: %w", err)
	}
	return nil
}

// TODO: add Prometheus metrics for commitQCs written and truncated.
package persist

import (
	"fmt"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const commitqcsDir = "commitqcs"

// commitqcsWALName identifies the commitQC WAL in metrics.
const commitqcsWALName = "autobahn_commitqcs"

// LoadedCommitQC is a CommitQC loaded from disk during state restoration.
type LoadedCommitQC struct {
	Index types.RoadIndex
	QC    *types.CommitQC
}

// commitQCState is the mutable state protected by CommitQCPersister's mutex.
type commitQCState struct {
	wal  utils.Option[seiwal.WAL[*types.CommitQC]]
	next types.RoadIndex
	// Whether a QC has been appended since the last flush, so a prune that re-persists its anchor is
	// still made durable while a run of duplicates costs no fsync.
	unflushed bool
}

// persistCommitQC schedules a CommitQC for the WAL under its own road index. The QC is not durable
// until flush returns. Caller must hold the lock.
// Duplicates (idx < next) are silently ignored for idempotent startup.
// Gaps (idx > next) return an error.
func (s *commitQCState) persistCommitQC(qc *types.CommitQC) error {
	idx := qc.Index()
	if idx < s.next {
		return nil
	}
	if idx > s.next {
		return fmt.Errorf("commitqc %d out of sequence (next=%d)", idx, s.next)
	}
	if w, ok := s.wal.Get(); ok {
		if err := w.Append(uint64(idx), qc); err != nil {
			return fmt.Errorf("persist commitqc %d: %w", idx, err)
		}
		s.unflushed = true
	}
	s.next = idx + 1
	return nil
}

// flush makes every QC scheduled by persistCommitQC durable. Caller must hold the lock.
func (s *commitQCState) flush() error {
	w, ok := s.wal.Get()
	if !ok || !s.unflushed {
		return nil
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush commitqc WAL: %w", err)
	}
	s.unflushed = false
	return nil
}

// deleteBefore prunes WAL entries below the anchor's index, then re-persists the anchor for crash
// recovery. Pruning is lazy, so entries below the anchor may remain on disk until the file holding
// them falls entirely below the threshold. Caller must hold the lock.
func (s *commitQCState) deleteBefore(anchor *types.CommitQC) error {
	idx := anchor.Index()
	if w, ok := s.wal.Get(); ok {
		if err := w.PruneBefore(uint64(idx)); err != nil {
			return fmt.Errorf("prune commitqc WAL before %d: %w", idx, err)
		}
	}
	if idx > s.next {
		// The anchor moved past every QC persisted so far, so the next QC to persist is the anchor
		// itself. That leaves a gap, which the WAL is configured to permit.
		s.next = idx
	}
	return s.persistCommitQC(anchor)
}

// CommitQCPersister manages CommitQC persistence using a WAL.
// Each QC is stored under its own RoadIndex, so no mapping between append order
// and RoadIndex is needed.
// When wal is None, all disk I/O is skipped but cursor tracking still works.
type CommitQCPersister struct {
	state utils.Mutex[*commitQCState]
}

// NewCommitQCPersister opens (or creates) a WAL in the commitqcs/ subdirectory
// and replays all persisted entries. Returns the persister and a sorted slice of
// loaded CommitQCs. A torn entry at the end of the WAL, left by a crash
// mid-write, is discarded by the WAL on open.
//
// The QCs returned are a superset of what the prune anchor considers live:
// pruning is lazy, so QCs the anchor has moved past can still be on disk. QCs
// stranded behind a gap are dropped, but those directly below the anchor are
// indistinguishable from live ones here and are filtered by the caller, which is
// what holds the anchor.
//
// When stateDir is None, returns a no-op persister.
//
// After crash recovery with an empty WAL, LoadNext() returns 0. The caller MUST
// use MaybePruneAndPersist with the prune CommitQC in Anchor to re-establish the
// cursor and re-persist the anchor's CommitQC before appending more QCs.
func NewCommitQCPersister(stateDir utils.Option[string]) (*CommitQCPersister, []LoadedCommitQC, error) {
	sd, ok := stateDir.Get()
	if !ok {
		return &CommitQCPersister{state: utils.NewMutex(&commitQCState{})}, nil, nil
	}
	dir := filepath.Join(sd, commitqcsDir)
	wal, err := openWAL(dir, commitqcsWALName, types.CommitQCConv, targetFileSize)
	if err != nil {
		return nil, nil, fmt.Errorf("open commitqc WAL in %s: %w", dir, err)
	}

	s := &commitQCState{wal: utils.Some(wal)}
	loaded, err := loadAllCommitQCs(s)
	if err != nil {
		_ = wal.Close()
		return nil, nil, err
	}
	if len(loaded) > 0 {
		s.next = loaded[len(loaded)-1].Index + 1
	}
	return &CommitQCPersister{state: utils.NewMutex(s)}, loaded, nil
}

// LoadNext returns the road index of the first CommitQC that has not been
// persisted (exclusive upper bound of what's on disk).
func (cp *CommitQCPersister) LoadNext() types.RoadIndex {
	for s := range cp.state.Lock() {
		return s.next
	}
	panic("unreachable")
}

// MaybePruneAndPersist optionally truncates the WAL and/or appends new
// CommitQCs, depending on which arguments are present:
//
//   - anchor set, commitQCs non-empty: truncate WAL below anchor, re-persist
//     the anchor QC for crash recovery, then append new QCs (runtime path).
//   - anchor set, commitQCs empty:     truncate and re-persist anchor only
//     (startup prune path).
//   - anchor empty, commitQCs non-empty: append only, no truncation.
//   - anchor empty, commitQCs empty:     no-op.
//
// Pruning is lazy, so QCs below the anchor may remain on disk for a while; the anchor is what defines
// which of them are live.
//
// The lock is held for the entire prune-then-append sequence, so callers
// need not coordinate ordering.
// afterEach, when present, is called once per QC in commitQCs in order, after the batch has been
// flushed — never before, because an append is not durable until then and afterEach is what releases a
// QC to the rest of consensus. It is invoked while the lock is held, so it must not re-enter the
// persister. If any append fails, afterEach is not called for the batch at all.
func (cp *CommitQCPersister) MaybePruneAndPersist(
	anchor utils.Option[*types.CommitQC],
	commitQCs []*types.CommitQC,
	afterEach utils.Option[func(*types.CommitQC)],
) error {
	for s := range cp.state.Lock() {
		if qc, ok := anchor.Get(); ok {
			if err := s.deleteBefore(qc); err != nil {
				return err
			}
		}
		for _, c := range commitQCs {
			if err := s.persistCommitQC(c); err != nil {
				return err
			}
		}
		// persistCommitQC only schedules the write, so the QCs are not durable — and must not be
		// reported as persisted — until this returns.
		if err := s.flush(); err != nil {
			return err
		}
		if fn, ok := afterEach.Get(); ok {
			for _, c := range commitQCs {
				fn(c)
			}
		}
		return nil
	}
	panic("unreachable")
}

// Close shuts down the WAL. Safe to call multiple times (idempotent).
func (cp *CommitQCPersister) Close() error {
	for s := range cp.state.Lock() {
		w, ok := s.wal.Get()
		if !ok {
			return nil // no-op persister or already closed
		}
		s.wal = utils.None[seiwal.WAL[*types.CommitQC]]()
		return w.Close()
	}
	panic("unreachable")
}

// loadAllCommitQCs reads the WAL and returns the QCs that are still live.
//
// QCs stranded below a lazy prune are discarded: a gap in the stored road indices marks where pruning
// has already logically removed everything before it.
func loadAllCommitQCs(s *commitQCState) ([]LoadedCommitQC, error) {
	w, ok := s.wal.Get()
	if !ok {
		return nil, nil // no-op persister (persistence disabled)
	}
	entries, err := readAll(w)
	if err != nil {
		return nil, fmt.Errorf("read commitqc WAL: %w", err)
	}
	live := contiguousSuffix(entries)

	loaded := make([]LoadedCommitQC, 0, len(live))
	for _, entry := range live {
		loaded = append(loaded, LoadedCommitQC{Index: entry.value.Index(), QC: entry.value})
	}
	return loaded, nil
}

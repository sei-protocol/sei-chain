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

// commitqcsWALMetrics is whether the commitQC WAL records metrics. One instance holds the name above, so
// unlike the lane WALs (see blocksWALName) it has nothing to collide with.
const commitqcsWALMetrics = true

// commitQCState is the mutable state protected by CommitQCPersister's mutex.
type commitQCState struct {
	wal       utils.Option[seiwal.WAL[*types.CommitQC]]
	persisted types.RoadRange
	// Whether a QC has been appended since the last flush, so a prune that re-persists its anchor is
	// still made durable while a run of duplicates costs no fsync.
	unflushed bool
}

// persistCommitQC schedules a CommitQC for the WAL under its own road index. The QC is not durable
// until flush returns. Caller must hold the lock.
// Duplicates (idx < next) are silently ignored for idempotent startup.
// Gaps (idx > next) return an error.
func (s *commitQCState) persist(qc *types.CommitQC) error {
	idx := qc.Index()
	if idx < s.persisted.Next {
		return nil
	}
	if idx > s.persisted.Next {
		return fmt.Errorf("commitqc %d out of sequence (next=%d)", idx, s.persisted.Next)
	}
	if w, ok := s.wal.Get(); ok {
		if err := w.Append(uint64(idx), qc); err != nil {
			return fmt.Errorf("persist commitqc %d: %w", idx, err)
		}
		s.unflushed = true
	}
	s.persisted.Next += 1
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

// deleteBefore prunes WAL entries below idx and advances the retained range so that idx becomes the
// oldest live index. An idx at or below the oldest retained index prunes nothing.
//
// Pruning is lazy, so entries below idx may remain on disk until the file holding them falls entirely
// below the threshold. Caller must hold the lock.
func (s *commitQCState) deleteBefore(idx types.RoadIndex) error {
	if idx <= s.persisted.First {
		return nil
	}
	if w, ok := s.wal.Get(); ok {
		if err := w.PruneBefore(uint64(idx)); err != nil {
			return fmt.Errorf("prune commitqc WAL before %d: %w", idx, err)
		}
	}
	s.persisted.First = idx
	s.persisted.Next = max(s.persisted.First, s.persisted.Next)
	return nil
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
// After crash recovery with an empty WAL, Next() returns 0. The caller MUST
// re-establish the cursor by calling PruneAndPersist with deleteBefore set to the
// first index it intends to append; appending above Next is rejected as a gap.
func NewCommitQCPersister(stateDir utils.Option[string]) (*CommitQCPersister, []*types.CommitQC, error) {
	sd, ok := stateDir.Get()
	if !ok {
		return &CommitQCPersister{state: utils.NewMutex(&commitQCState{})}, nil, nil
	}
	dir := filepath.Join(sd, commitqcsDir)
	wal, err := openWAL(dir, commitqcsWALName, types.CommitQCConv, targetFileSize, commitqcsWALMetrics)
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
		s.persisted = types.RoadRange{
			First: loaded[0].Index(),
			Next:  loaded[len(loaded)-1].Index() + 1,
		}
	}
	return &CommitQCPersister{state: utils.NewMutex(s)}, loaded, nil
}

// LoadNext returns the road index of the first CommitQC that has not been
// persisted (exclusive upper bound of what's on disk).
func (cp *CommitQCPersister) Next() types.RoadIndex {
	for s := range cp.state.Lock() {
		return s.persisted.Next
	}
	panic("unreachable")
}

// PruneAndPersist prunes the WAL below deleteBefore, then appends commitQCs in order and flushes them
// as one batch. An empty commitQCs prunes only, and a deleteBefore at or below the oldest retained
// index prunes nothing. Duplicates below the cursor are skipped; a gap above it is an error.
//
// Pruning is lazy, so QCs below deleteBefore may remain on disk until the file holding them falls
// entirely below the threshold.
//
// The lock is held for the entire prune-then-append sequence, so callers
// need not coordinate ordering.
func (cp *CommitQCPersister) PruneAndPersist(deleteBefore types.RoadIndex, commitQCs []*types.CommitQC) error {
	for s := range cp.state.Lock() {
		if err := s.deleteBefore(deleteBefore); err != nil {
			return err
		}
		for _, c := range commitQCs {
			if err := s.persist(c); err != nil {
				return err
			}
		}
		// persistCommitQC only schedules the write, so the QCs are not durable — and must not be
		// reported as persisted — until this returns.
		if err := s.flush(); err != nil {
			return err
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
func loadAllCommitQCs(s *commitQCState) ([]*types.CommitQC, error) {
	w, ok := s.wal.Get()
	if !ok {
		return nil, nil // no-op persister (persistence disabled)
	}
	entries, err := readAll(w)
	if err != nil {
		return nil, fmt.Errorf("read commitqc WAL: %w", err)
	}
	var loaded []*types.CommitQC
	for _, entry := range contiguousSuffix(entries) {
		loaded = append(loaded, entry.value)
	}
	return loaded, nil
}

// TODO: add Prometheus metrics for commitQCs written and truncated.
package persist

import (
	"fmt"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const commitqcsDir = "commitqcs"

// LoadedCommitQC is a CommitQC loaded from disk during state restoration.
type LoadedCommitQC struct {
	Index types.RoadIndex
	QC    *types.CommitQC
}

// commitQCState is the mutable state protected by CommitQCPersister's mutex.
type commitQCState struct {
	iw   utils.Option[*indexedWAL[*types.CommitQC]]
	next types.RoadIndex
}

// persistCommitQC writes a CommitQC to the WAL. Caller must hold the lock.
// Duplicates (idx < next) are silently ignored for idempotent startup.
// Gaps (idx > next) return an error (breaks linear index mapping).
func (s *commitQCState) persistCommitQC(qc *types.CommitQC) error {
	idx := qc.Index()
	if idx < s.next {
		return nil
	}
	if idx > s.next {
		return fmt.Errorf("commitqc %d out of sequence (next=%d)", idx, s.next)
	}
	if iw, ok := s.iw.Get(); ok {
		if err := iw.Write(qc); err != nil {
			return fmt.Errorf("persist commitqc %d: %w", idx, err)
		}
	}
	s.next = idx + 1
	return nil
}

// deleteBefore truncates WAL entries below the anchor's index, then
// re-persists the anchor for crash recovery. Caller must hold the lock.
func (s *commitQCState) deleteBefore(anchor *types.CommitQC) error {
	idx := anchor.Index()
	iw, ok := s.iw.Get()
	if idx >= s.next {
		s.next = idx
		if ok && iw.Count() > 0 {
			if err := iw.TruncateAll(); err != nil {
				return err
			}
		}
	} else if ok && iw.Count() > 0 {
		firstRoadIndex := s.next - types.RoadIndex(iw.Count())
		if idx > firstRoadIndex {
			walIdx := iw.FirstIdx() + uint64(idx-firstRoadIndex)
			if err := iw.TruncateBefore(walIdx, func(entry *types.CommitQC) error {
				if entry.Index() != idx {
					return fmt.Errorf("commitqc at WAL index %d has road index %d, expected %d (index mapping broken)", walIdx, entry.Index(), idx)
				}
				return nil
			}); err != nil {
				return fmt.Errorf("truncate commitqc WAL before %d: %w", walIdx, err)
			}
		}
	}
	return s.persistCommitQC(anchor)
}

// CommitQCPersister manages CommitQC persistence using a WAL.
// Entries are appended in order; each entry is self-describing (the serialized
// CommitQC contains its RoadIndex). The WAL index is append order, not
// RoadIndex — the indexedWAL tracks first/next indices to enable truncation.
// When iw is None, all disk I/O is skipped but cursor tracking still works.
type CommitQCPersister struct {
	state utils.Mutex[*commitQCState]
}

// NewCommitQCPersister opens (or creates) a WAL in the commitqcs/ subdirectory
// and replays all persisted entries. Returns the persister and a sorted slice of
// loaded CommitQCs. Corrupt tail entries are auto-truncated by the WAL library.
// When stateDir is None, returns a no-op persister.
//
// After crash recovery with an empty WAL (e.g. TruncateAll completed but no
// new write followed), LoadNext() returns 0. The caller MUST use
// MaybePruneAndPersist with the prune CommitQC in Anchor to re-establish the
// cursor and re-persist the anchor's CommitQC before appending more QCs.
func NewCommitQCPersister(stateDir utils.Option[string]) (*CommitQCPersister, []LoadedCommitQC, error) {
	sd, ok := stateDir.Get()
	if !ok {
		return &CommitQCPersister{state: utils.NewMutex(&commitQCState{})}, nil, nil
	}
	dir := filepath.Join(sd, commitqcsDir)
	iw, err := openIndexedWAL(dir, types.CommitQCConv)
	if err != nil {
		return nil, nil, fmt.Errorf("open commitqc WAL in %s: %w", dir, err)
	}

	s := &commitQCState{iw: utils.Some(iw)}
	loaded, err := loadAllCommitQCs(s)
	if err != nil {
		_ = iw.Close()
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
// The lock is held for the entire truncate-then-append sequence, so callers
// need not coordinate ordering.
// afterEach, when present, is called after each successful append. It is
// invoked while the lock is held, so it must not re-enter the persister.
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
		fn, hasFn := afterEach.Get()
		for _, c := range commitQCs {
			if err := s.persistCommitQC(c); err != nil {
				return err
			}
			if hasFn {
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
		iw, ok := s.iw.Get()
		if !ok {
			return nil // no-op persister or already closed
		}
		s.iw = utils.None[*indexedWAL[*types.CommitQC]]()
		return iw.Close()
	}
	panic("unreachable")
}

func loadAllCommitQCs(s *commitQCState) ([]LoadedCommitQC, error) {
	iw, ok := s.iw.Get()
	if !ok {
		return nil, nil // no-op persister (persistence disabled)
	}
	entries, err := iw.ReadAll()
	if err != nil {
		return nil, err
	}
	loaded := make([]LoadedCommitQC, 0, len(entries))
	for i, qc := range entries {
		if i > 0 && qc.Index() != loaded[i-1].Index+1 {
			return nil, fmt.Errorf("gap in commitqcs: index %d follows %d", qc.Index(), loaded[i-1].Index)
		}
		loaded = append(loaded, LoadedCommitQC{Index: qc.Index(), QC: qc})
	}
	return loaded, nil
}

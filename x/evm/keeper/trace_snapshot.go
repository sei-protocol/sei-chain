package keeper

import (
	"sync"

	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// TraceSnapshotStore holds bounded in-memory SC snapshots keyed by block height.
// Each entry is a Committer.Copy() sharing COW nodes with the live memiavl tree.
//
// Operational signals to watch when this store is in use:
//   - memiavl gauges MemNodeTotalSize / NumOfMemNode: rise if held snapshots
//     pin too many COW nodes (bigger window or higher TPS amplifies this).
//   - trace baker counters (BakedCount / DroppedCount / FailedCount): if
//     DroppedCount climbs or BakedCount lags the chain tip, the baker is
//     falling behind and trace cache hit rate will drop.
type TraceSnapshotStore struct {
	mu        sync.Mutex
	snapshots map[int64]sctypes.Committer
	window    int64
	latest    int64
}

type snapshotRefReleaser interface {
	ReleaseSnapshotRefs() error
}

func NewTraceSnapshotStore(window int64) *TraceSnapshotStore {
	if window <= 0 {
		window = 64
	}
	return &TraceSnapshotStore{
		snapshots: make(map[int64]sctypes.Committer),
		window:    window,
	}
}

// Put records a snapshot and evicts entries older than (latest - window).
// In-flight traces use Lease, so evicted map entries can be closed explicitly.
func (s *TraceSnapshotStore) Put(height int64, snap sctypes.Committer) {
	if s == nil || snap == nil {
		return
	}
	var toRelease []sctypes.Committer
	s.mu.Lock()

	if old := s.snapshots[height]; old != nil {
		toRelease = append(toRelease, old)
	}
	s.snapshots[height] = snap
	if height > s.latest {
		s.latest = height
	}
	cutoff := s.latest - s.window
	for h := range s.snapshots {
		if h <= cutoff {
			toRelease = append(toRelease, s.snapshots[h])
			delete(s.snapshots, h)
		}
	}
	s.mu.Unlock()

	for _, snap := range toRelease {
		releaseSnapshotRefs(snap)
	}
}

// Lease returns an owned snapshot copy and a release function for trace state.
func (s *TraceSnapshotStore) Lease(height int64) (sctypes.Committer, func()) {
	if s == nil {
		return nil, func() {}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.snapshots[height]
	if snap == nil {
		return nil, func() {}
	}
	leased := snap.Copy()
	if leased == nil {
		return nil, func() {}
	}
	return leased, func() { releaseSnapshotRefs(leased) }
}

func (s *TraceSnapshotStore) Get(height int64) sctypes.Committer {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshots[height]
}

// Close releases all retained snapshots.
func (s *TraceSnapshotStore) Close() {
	if s == nil {
		return
	}
	var toRelease []sctypes.Committer
	s.mu.Lock()
	for h := range s.snapshots {
		toRelease = append(toRelease, s.snapshots[h])
		delete(s.snapshots, h)
	}
	s.mu.Unlock()

	for _, snap := range toRelease {
		releaseSnapshotRefs(snap)
	}
}

func releaseSnapshotRefs(snap sctypes.Committer) {
	if snap == nil {
		return
	}
	if releaser, ok := snap.(snapshotRefReleaser); ok {
		_ = releaser.ReleaseSnapshotRefs()
		return
	}
	_ = snap.Close()
}

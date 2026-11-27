package keeper

import (
	"sync"
)

// TraceSnapshot is an in-memory SC snapshot that a TraceSnapshotStore retains, copies for leases, and
// releases.
type TraceSnapshot[S any] interface {
	comparable
	// Copy returns an independent snapshot of the same state, or the zero value when none can be made.
	Copy() S
	// ReleaseSnapshotRefs drops the snapshot's references to state it shares with the live store.
	ReleaseSnapshotRefs() error
}

// TraceSnapshotStore holds bounded in-memory SC snapshots keyed by block height.
// Each entry is a Copy() sharing COW nodes with the live memiavl tree.
//
// Operational signals to watch when this store is in use:
//   - memiavl gauges MemNodeTotalSize / NumOfMemNode: rise if held snapshots
//     pin too many COW nodes (bigger window or higher TPS amplifies this).
//   - trace baker counters (BakedCount / DroppedCount / FailedCount): if
//     DroppedCount climbs or BakedCount lags the chain tip, the baker is
//     falling behind and trace cache hit rate will drop.
type TraceSnapshotStore[S TraceSnapshot[S]] struct {
	mu        sync.Mutex
	snapshots map[int64]S
	window    int64
	latest    int64
}

func NewTraceSnapshotStore[S TraceSnapshot[S]](window int64) *TraceSnapshotStore[S] {
	if window <= 0 {
		window = 64
	} else if window < 2 {
		window = 2
	}
	return &TraceSnapshotStore[S]{
		snapshots: make(map[int64]S),
		window:    window,
	}
}

// Put records a snapshot and evicts entries older than (latest - window).
// In-flight traces use Lease, so evicted map entries can be closed explicitly.
func (s *TraceSnapshotStore[S]) Put(height int64, snap S) {
	var none S
	if s == nil || snap == none {
		return
	}
	var toRelease []S
	s.mu.Lock()

	if old, ok := s.snapshots[height]; ok {
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
func (s *TraceSnapshotStore[S]) Lease(height int64) (S, func()) {
	var none S
	if s == nil {
		return none, func() {}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.snapshots[height]
	if !ok {
		return none, func() {}
	}
	leased := snap.Copy()
	if leased == none {
		return none, func() {}
	}
	return leased, func() { releaseSnapshotRefs(leased) }
}

func (s *TraceSnapshotStore[S]) Get(height int64) S {
	var none S
	if s == nil {
		return none
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshots[height]
}

// Close releases all retained snapshots.
func (s *TraceSnapshotStore[S]) Close() {
	if s == nil {
		return
	}
	var toRelease []S
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

func releaseSnapshotRefs[S TraceSnapshot[S]](snap S) {
	var none S
	if snap == none {
		return
	}
	if err := snap.ReleaseSnapshotRefs(); err != nil {
		logger.Warn("failed to release trace snapshot refs", "err", err)
	}
}

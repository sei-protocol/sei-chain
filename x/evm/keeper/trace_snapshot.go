package keeper

import (
	"sync"

	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// TraceSnapshotStore holds in-memory SC snapshots keyed by block height for
// the trace baker. Each snapshot is a Committer.Copy() — a persistent-tree
// reference that shares unmodified nodes with the live tree (memiavl COW).
//
// Bounded by a configurable window. When the window slides past a height,
// the snapshot is dropped, releasing its hold on COW nodes so memiavl can
// reclaim them at the next commit.
//
// Safe for concurrent Put/Get/Drop. The captured snapshots themselves are
// safe for concurrent reads via the underlying memiavl tree.
type TraceSnapshotStore struct {
	mu        sync.Mutex
	snapshots map[int64]sctypes.Committer
	window    int64 // keep entries with height in (latest-window, latest]
	latest    int64
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

// Put records a snapshot for the given height and evicts everything older
// than (height - window). Safe to call from EndBlock; sub-microsecond.
func (s *TraceSnapshotStore) Put(height int64, snap sctypes.Committer) {
	if s == nil || snap == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.snapshots[height]; ok {
		// Prefer the newer capture, drop the old one.
		_ = existing.Close()
	}
	s.snapshots[height] = snap
	if height > s.latest {
		s.latest = height
	}
	cutoff := s.latest - s.window
	for h, sn := range s.snapshots {
		if h <= cutoff {
			_ = sn.Close()
			delete(s.snapshots, h)
		}
	}
}

// Get returns the snapshot for the requested height, or nil if absent.
// Safe on a nil receiver.
func (s *TraceSnapshotStore) Get(height int64) sctypes.Committer {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshots[height]
}

// Has reports whether a snapshot is available at the given height.
func (s *TraceSnapshotStore) Has(height int64) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.snapshots[height]
	return ok
}

// Len returns the current number of held snapshots (mostly for diagnostics
// / metrics). Safe on a nil receiver.
func (s *TraceSnapshotStore) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.snapshots)
}

// Close releases all held snapshots. Idempotent. Safe on a nil receiver.
func (s *TraceSnapshotStore) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for h, sn := range s.snapshots {
		_ = sn.Close()
		delete(s.snapshots, h)
	}
}

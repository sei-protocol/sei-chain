package keeper

import (
	"sync"

	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// TraceSnapshotStore holds bounded in-memory SC snapshots keyed by block height.
// Each entry is a Committer.Copy() sharing COW nodes with the live memiavl tree.
type TraceSnapshotStore struct {
	mu        sync.Mutex
	snapshots map[int64]sctypes.Committer
	window    int64
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

// Put records a snapshot and evicts entries older than (latest - window).
// Eviction drops only the map ref; the memiavl Tree finalizer releases the
// snapshot ref so callers don't race Close with in-flight reads.
func (s *TraceSnapshotStore) Put(height int64, snap sctypes.Committer) {
	if s == nil || snap == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshots[height] = snap
	if height > s.latest {
		s.latest = height
	}
	cutoff := s.latest - s.window
	for h := range s.snapshots {
		if h <= cutoff {
			delete(s.snapshots, h)
		}
	}
}

func (s *TraceSnapshotStore) Get(height int64) sctypes.Committer {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshots[height]
}

// Close drops all map refs; snapshot memory is reclaimed by the memiavl finalizer.
func (s *TraceSnapshotStore) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for h := range s.snapshots {
		delete(s.snapshots, h)
	}
}

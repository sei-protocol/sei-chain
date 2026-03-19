package flatkv

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/dbcache"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// loadLocalMeta loads the local metadata from a DB, or returns default if not present.
func loadLocalMeta(db dbcache.Cache) (*LocalMeta, error) {
	val, found, err := db.Get(DBLocalMetaKey, false)
	if err != nil {
		return nil, fmt.Errorf("failed to read local meta: %w", err)
	}
	if !found {
		return &LocalMeta{CommittedVersion: 0}, nil
	}
	return UnmarshalLocalMeta(val)
}

// loadGlobalVersion reads the global committed version from metadata DB.
// Returns 0 if not found (fresh start).
func (s *CommitStore) loadGlobalVersion() (int64, error) {
	data, found, err := s.metadataDB.Get([]byte(MetaGlobalVersion), false)
	if err != nil {
		return 0, nil
	}
	if !found {
		return 0, nil
	}
	if len(data) != 8 {
		return 0, fmt.Errorf("invalid global version length: got %d, want 8", len(data))
	}
	v := binary.BigEndian.Uint64(data)
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("global version overflow: %d exceeds max int64", v)
	}
	return int64(v), nil //nolint:gosec // overflow checked above
}

// loadGlobalLtHash reads the global committed LtHash from metadata DB.
// Returns nil if not found (fresh start).
func (s *CommitStore) loadGlobalLtHash() (*lthash.LtHash, error) {
	data, found, err := s.metadataDB.Get([]byte(MetaGlobalLtHash), false)
	if err != nil {
		return nil, fmt.Errorf("failed to read global lthash: %w", err)
	}
	if !found {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read global lthash: %w", err)
	}
	return lthash.Unmarshal(data)
}

// commitGlobalMetadata atomically commits global version and LtHash to metadata DB.
// This is the global watermark written AFTER all per-DB commits succeed.
func (s *CommitStore) commitGlobalMetadata(version int64, hash *lthash.LtHash) error {
	versionBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(versionBuf, uint64(version)) //nolint:gosec // version is always non-negative
	s.metadataDB.Set([]byte(MetaGlobalVersion), versionBuf)
	s.metadataDB.Set([]byte(MetaGlobalLtHash), hash.Marshal())

	// Force the metadata cache to flush down to the DB by taking a snapshot and releasing it.
	// TODO before merge:
	//   - The semantics of this are not obvious, we need to expand godocs to make it more clear what's going on.
	//   - Double check with team on the crash recovery story here, since we're now making this asynchronous.
	snapshot, err := s.metadataDB.Snapshot()
	if err != nil {
		return fmt.Errorf("failed to take snapshot: %w", err)
	}
	err = snapshot.Release()
	if err != nil {
		return fmt.Errorf("failed to release snapshot: %w", err)
	}
	return nil
}

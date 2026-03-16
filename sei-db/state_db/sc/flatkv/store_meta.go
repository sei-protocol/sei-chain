package flatkv

import (
	"encoding/binary"
	"fmt"
	"math"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// loadLocalMeta loads the local metadata from a DB, or returns default if not present.
func loadLocalMeta(db types.KeyValueDB) (*LocalMeta, error) {
	val, err := db.Get(DBLocalMetaKey)
	if err != nil && !errorutils.IsNotFound(err) {
		return nil, fmt.Errorf("could not get DBLocalMetaKey: %w", err)
	}
	if errorutils.IsNotFound(err) || val == nil {
		return &LocalMeta{CommittedVersion: 0}, nil
	}
	return UnmarshalLocalMeta(val)
}

// loadGlobalVersion reads the global committed version from metadata DB.
// Returns 0 if not found (fresh start).
func (s *CommitStore) loadGlobalVersion() (int64, error) {
	data, err := s.metadataDB.Get([]byte(MetaGlobalVersion))
	if errorutils.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read global version: %w", err)
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
	data, err := s.metadataDB.Get([]byte(MetaGlobalLtHash))
	if errorutils.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read global lthash: %w", err)
	}
	return lthash.Unmarshal(data)
}

// commitGlobalMetadata atomically commits global version, global LtHash,
// and per-DB LtHashes to metadata DB.
func (s *CommitStore) commitGlobalMetadata(version int64, hash *lthash.LtHash) error {
	batch := s.metadataDB.NewBatch()
	defer func() { _ = batch.Close() }()

	versionBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(versionBuf, uint64(version)) //nolint:gosec // version is always non-negative

	if err := batch.Set([]byte(MetaGlobalVersion), versionBuf); err != nil {
		return fmt.Errorf("failed to set global version: %w", err)
	}

	lthashBytes := hash.Marshal()
	if err := batch.Set([]byte(MetaGlobalLtHash), lthashBytes); err != nil {
		return fmt.Errorf("failed to set global lthash: %w", err)
	}

	for dbDir, metaKey := range perDBLtHashKeys {
		if h := s.perDBCommittedLtHash[dbDir]; h != nil {
			if err := batch.Set([]byte(metaKey), h.Marshal()); err != nil {
				return fmt.Errorf("failed to set %s lthash: %w", dbDir, err)
			}
		}
	}

	return batch.Commit(types.WriteOptions{Sync: s.config.Fsync})
}

// newPerDBLtHashMap returns a map with a fresh zero LtHash for each data DB.
func newPerDBLtHashMap() map[string]*lthash.LtHash {
	m := make(map[string]*lthash.LtHash, len(perDBLtHashKeys))
	for dbDir := range perDBLtHashKeys {
		m[dbDir] = lthash.New()
	}
	return m
}

// snapshotLtHashes clones working hashes (global + per-DB) into committed state.
func (s *CommitStore) snapshotLtHashes() {
	s.committedLtHash = s.workingLtHash.Clone()
	for dbDir, h := range s.perDBWorkingLtHash {
		s.perDBCommittedLtHash[dbDir] = h.Clone()
	}
}

// loadPerDBLtHashes reads per-DB LtHashes from metadataDB.
// If a key is not found (fresh start), initializes to zero.
func (s *CommitStore) loadPerDBLtHashes() error {
	for dbDir, metaKey := range perDBLtHashKeys {
		data, err := s.metadataDB.Get([]byte(metaKey))
		if errorutils.IsNotFound(err) {
			logger.Warn("No lattice hash found for DB, initializing to fresh hash", "db", dbDir)
			s.perDBCommittedLtHash[dbDir] = lthash.New()
			s.perDBWorkingLtHash[dbDir] = lthash.New()
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to read %s lthash: %w", dbDir, err)
		}
		h, err := lthash.Unmarshal(data)
		if err != nil {
			return fmt.Errorf("failed to unmarshal %s lthash: %w", dbDir, err)
		}
		s.perDBCommittedLtHash[dbDir] = h
		s.perDBWorkingLtHash[dbDir] = h.Clone()
	}
	return nil
}

package flatkv

import (
	"encoding/binary"
	"fmt"
	"math"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// loadLocalMeta loads the local metadata from a DB, or returns default if not present.
func loadLocalMeta(db db_engine.KeyValueDB) (*LocalMeta, error) {
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

// commitGlobalMetadata atomically commits global version and LtHash to metadata DB.
// This is the global watermark written AFTER all per-DB commits succeed.
func (s *CommitStore) commitGlobalMetadata(version int64, hash *lthash.LtHash) error {
	batch := s.metadataDB.NewBatch()
	defer func() { _ = batch.Close() }()

	// Encode version (version should always be non-negative in practice)
	versionBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(versionBuf, uint64(version)) //nolint:gosec // version is always non-negative

	// Write global metadata
	if err := batch.Set([]byte(MetaGlobalVersion), versionBuf); err != nil {
		return fmt.Errorf("failed to set global version: %w", err)
	}

	lthashBytes := hash.Marshal()
	if err := batch.Set([]byte(MetaGlobalLtHash), lthashBytes); err != nil {
		return fmt.Errorf("failed to set global lthash: %w", err)
	}

	// Atomic commit with fsync
	return batch.Commit(db_engine.WriteOptions{Sync: true})
}

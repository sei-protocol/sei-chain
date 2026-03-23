package flatkv

import (
	"encoding/binary"
	"fmt"
	"math"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// versionToBytes encodes a non-negative version as 8-byte big-endian.
// Panics on negative input to catch programming errors early.
// Only called from internal commit/test paths — never with untrusted input.
func versionToBytes(v int64) []byte {
	if v < 0 {
		panic(fmt.Sprintf("flatkv: negative version %d", v))
	}
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v)) //nolint:gosec // guarded above
	return b
}

// loadLocalMeta loads per-DB metadata by reading separate keys.
func loadLocalMeta(db types.KeyValueDB) (*LocalMeta, error) {
	meta := &LocalMeta{}

	versionData, err := db.Get(metaVersionKey)
	if err != nil {
		if errorutils.IsNotFound(err) {
			return &LocalMeta{CommittedVersion: 0}, nil
		}
		return nil, fmt.Errorf("could not read meta version: %w", err)
	}
	if len(versionData) != 8 {
		return nil, fmt.Errorf("invalid meta version length: got %d, want 8", len(versionData))
	}
	meta.CommittedVersion = int64(binary.BigEndian.Uint64(versionData)) //nolint:gosec // version won't exceed int64 max

	hashData, err := db.Get(metaLtHashKey)
	if err != nil && !errorutils.IsNotFound(err) {
		return nil, fmt.Errorf("could not read meta hash: %w", err)
	}
	if err == nil && hashData != nil {
		h, err := lthash.Unmarshal(hashData)
		if err != nil {
			return nil, fmt.Errorf("unmarshal meta hash: %w", err)
		}
		meta.LtHash = h
	}

	return meta, nil
}

// writeLocalMetaToBatch writes per-DB metadata (version + LtHash) as separate keys.
func writeLocalMetaToBatch(batch types.Batch, version int64, ltHash *lthash.LtHash) error {
	if err := batch.Set(metaVersionKey, versionToBytes(version)); err != nil {
		return fmt.Errorf("set meta version: %w", err)
	}
	if ltHash != nil {
		if err := batch.Set(metaLtHashKey, ltHash.Marshal()); err != nil {
			return fmt.Errorf("set meta hash: %w", err)
		}
	}
	return nil
}

// loadGlobalVersion reads the global committed version from metadata DB.
// Returns 0 if not found (fresh start).
func (s *CommitStore) loadGlobalVersion() (int64, error) {
	data, err := s.metadataDB.Get(metaVersionKey)
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
	data, err := s.metadataDB.Get(metaLtHashKey)
	if errorutils.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read global lthash: %w", err)
	}
	return lthash.Unmarshal(data)
}

// commitGlobalMetadata atomically commits global version and global LtHash
// to metadata DB. Per-DB LtHashes are stored in each DB's LocalMeta
// (committed atomically with data in commitBatches).
func (s *CommitStore) commitGlobalMetadata(version int64, hash *lthash.LtHash) error {
	batch := s.metadataDB.NewBatch()
	defer func() { _ = batch.Close() }()

	if err := batch.Set(metaVersionKey, versionToBytes(version)); err != nil {
		return fmt.Errorf("failed to set global version: %w", err)
	}
	if err := batch.Set(metaLtHashKey, hash.Marshal()); err != nil {
		return fmt.Errorf("failed to set global lthash: %w", err)
	}

	return batch.Commit(types.WriteOptions{Sync: s.config.Fsync})
}

// newPerDBLtHashMap returns a map with a fresh zero LtHash for each data DB.
func newPerDBLtHashMap() map[string]*lthash.LtHash {
	m := make(map[string]*lthash.LtHash, len(dataDBDirs))
	for _, dbDir := range dataDBDirs {
		m[dbDir] = lthash.New()
	}
	return m
}

package flatkv

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/dbcache"
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
func loadLocalMeta(db dbcache.Cache) (*LocalMeta, error) {
	meta := &LocalMeta{}

	versionData, found, err := db.Get(metaVersionKey, true)
	if err != nil {
		return nil, fmt.Errorf("could not read meta version: %w", err)
	}
	if !found {
		return &LocalMeta{CommittedVersion: 0}, nil
	}
	if len(versionData) != 8 {
		return nil, fmt.Errorf("invalid meta version length: got %d, want 8", len(versionData))
	}
	meta.CommittedVersion = int64(binary.BigEndian.Uint64(versionData)) //nolint:gosec // version won't exceed int64 max

	hashData, found, err := db.Get(metaLtHashKey, true)
	if err != nil {
		return nil, fmt.Errorf("could not read meta hash: %w", err)
	}
	if !found {
		return meta, nil
	}
	if hashData != nil {
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

// newPerDBLtHashMap returns a map with a fresh zero LtHash for each data DB.
func newPerDBLtHashMap() map[string]*lthash.LtHash {
	m := make(map[string]*lthash.LtHash, len(dataDBDirs))
	for _, dbDir := range dataDBDirs {
		m[dbDir] = lthash.New()
	}
	return m
}

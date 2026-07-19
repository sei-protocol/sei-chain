package flatkv

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// TestVerifyDBModuleMetadataOrphanStats ensures a stats entry for a module
// with no on-disk keys and no maintained hash cannot slip past verification.
// The hash-keyed residue loop alone would miss it.
func TestVerifyDBModuleMetadataOrphanStats(t *testing.T) {
	cs := &CommitStore{
		committedVersion:         1,
		perDBWorkingLtHash:       map[string]*lthash.LtHash{storageDBDir: lthash.New()},
		perDBModuleWorkingLtHash: map[string]map[string]*lthash.LtHash{storageDBDir: {}},
		perDBModuleWorkingStats: map[string]map[string]lthash.ModuleStats{
			storageDBDir: {
				"orphan": {KeyCount: 3, Bytes: 99},
			},
		},
	}

	_, err := cs.verifyDBModuleMetadata(storageDBDir, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "per-module stats")
	require.Contains(t, err.Error(), "orphan")
}

func TestVerifyDBModuleMetadataZeroResidueOK(t *testing.T) {
	cs := &CommitStore{
		committedVersion: 1,
		perDBWorkingLtHash: map[string]*lthash.LtHash{
			storageDBDir: lthash.New(),
		},
		perDBModuleWorkingLtHash: map[string]map[string]*lthash.LtHash{
			storageDBDir: {"gone": lthash.New()},
		},
		perDBModuleWorkingStats: map[string]map[string]lthash.ModuleStats{
			storageDBDir: {"gone": {}},
		},
	}

	root, err := cs.verifyDBModuleMetadata(storageDBDir, nil, nil)
	require.NoError(t, err)
	require.True(t, root.IsZero())
}

// TestVerifyLtHashIgnoresEmptyValueRows pins the membership predicate shared
// with foldChunk / serializeKV: a live Pebble row with an empty value is not
// part of the LtHash set and must not inflate the verification scan's stats
// (or hash) relative to incrementally maintained bookkeeping.
func TestVerifyLtHashIgnoresEmptyValueRows(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	commitStorageEntry(t, s, addrN(0x01), slotN(0x01), []byte{0xAA})

	// Plant an empty-value row that foldChunk would never count.
	emptyKey := storagePhysKey(addrN(0x02), slotN(0x02))
	require.NoError(t, s.storageDB.Set(emptyKey, nil, types.WriteOptions{}))

	require.NoError(t, VerifyLtHash(s))
}

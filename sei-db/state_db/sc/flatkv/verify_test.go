package flatkv

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// TestVerifyDBModuleMetadataOrphanStats ensures a stats entry for a module
// with no on-disk keys and no maintained hash cannot slip past verification.
// The hash-keyed residue loop alone would miss it.
func TestVerifyDBModuleMetadataOrphanStats(t *testing.T) {
	cs := &CommitStore{committedVersion: 1}
	maintained := &lthash.BlockHash{
		PerDB:     map[string]*lthash.LtHash{storageDBDir: lthash.New()},
		PerModule: map[string]map[string]*lthash.LtHash{storageDBDir: {}},
		PerModuleStats: map[string]map[string]lthash.ModuleStats{
			storageDBDir: {
				"orphan": {KeyCount: 3, Bytes: 99},
			},
		},
	}

	_, err := cs.verifyDBModuleMetadata(storageDBDir, maintained, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "per-module stats")
	require.Contains(t, err.Error(), "orphan")
}

func TestVerifyDBModuleMetadataZeroResidueOK(t *testing.T) {
	cs := &CommitStore{committedVersion: 1}
	maintained := &lthash.BlockHash{
		PerDB: map[string]*lthash.LtHash{
			storageDBDir: lthash.New(),
		},
		PerModule: map[string]map[string]*lthash.LtHash{
			storageDBDir: {"gone": lthash.New()},
		},
		PerModuleStats: map[string]map[string]lthash.ModuleStats{
			storageDBDir: {"gone": {}},
		},
	}

	root, err := cs.verifyDBModuleMetadata(storageDBDir, maintained, nil, nil)
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
	require.NoError(t, s.rawDBFor(storageDBDir).Set(emptyKey, nil, types.WriteOptions{}))

	require.NoError(t, VerifyLtHash(s))
}

// Verification must refuse a store with a staged block. The scan walks the stores, which merge the rows
// that block has staged, while the hashes it is compared against do not account for them yet — so
// proceeding reports an integrity failure on a store that is perfectly healthy.
//
// The message matters, not just the error: an unguarded verification does fail, but with a global
// mismatch, so asserting only that an error came back passes whether the guard works or not.
func TestVerifyLtHashRefusesStagedBlock(t *testing.T) {
	s := setupTestStore(t)
	defer func() { _ = s.Close() }()

	commitStorageEntry(t, s, addrN(0x01), slotN(0x01), []byte{0xAA})
	require.NoError(t, VerifyLtHash(s), "a committed store must verify")

	// Stage a block without committing it.
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		namedCS(storagePair(addrN(0x02), slotN(0x02), []byte{0xBB})),
	}))

	err := VerifyLtHash(s)
	require.Error(t, err, "verification must be refused while a block is staged")
	require.ErrorContains(t, err, "uncommitted writes",
		"the refusal must name the staged block, not report an integrity mismatch")

	// Committing clears it, and verification passes again.
	commitAndCheck(t, s)
	require.NoError(t, VerifyLtHash(s))
}

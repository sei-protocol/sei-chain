package operations

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
	"github.com/stretchr/testify/require"
)

func TestOpenFlatKVReadOnlyUsesTempClone(t *testing.T) {
	cfg := flatkvconfig.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), "flatkv")

	store, err := flatkv.NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	_, err = store.LoadVersion(0, false)
	require.NoError(t, err)

	nonceKey := evmNonceKey(0x11)
	nonceVal := nonceBytes(7)
	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: keys.EVMStoreKey,
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{{Key: nonceKey, Value: nonceVal}},
			},
		},
	}))
	_, err = store.Commit()
	require.NoError(t, err)

	toolStore, err := openFlatKVReadOnly(cfg.DataDir, 0)
	require.NoError(t, err, "tool open should not contend with the source store lock")
	defer func() { require.NoError(t, toolStore.Close()) }()

	require.Equal(t, int64(1), toolStore.Version())
	got, found := toolStore.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, found)
	require.Equal(t, nonceVal, got)
}

func TestVerifyCrossStoreMatchesAccountAndLegacyRows(t *testing.T) {
	flatCfg := flatkvconfig.DefaultTestConfig(t)
	flatCfg.DataDir = filepath.Join(t.TempDir(), "flatkv")

	flatStore, err := flatkv.NewCommitStore(t.Context(), flatCfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, flatStore.Close()) }()
	_, err = flatStore.LoadVersion(0, false)
	require.NoError(t, err)

	memDir := filepath.Join(t.TempDir(), "memiavl")
	memDB, err := memiavl.OpenDB(0, memiavl.Options{
		Dir:             memDir,
		CreateIfMissing: true,
		InitialStores:   []string{keys.EVMStoreKey, "bank"},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, memDB.Close()) }()

	addrKey := make([]byte, 20)
	addrKey[19] = 0x22
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addrKey)
	nonceVal := nonceBytes(9)
	codeHashKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addrKey)
	codeHashVal := make([]byte, vtype.CodeHashLen)
	codeHashVal[0] = 0xAB

	bankKey := []byte("supply")
	bankVal := []byte("100")

	changeSets := []*proto.NamedChangeSet{
		{
			Name: keys.EVMStoreKey,
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: nonceKey, Value: nonceVal},
					{Key: codeHashKey, Value: codeHashVal},
				},
			},
		},
		{
			Name: "bank",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: bankKey, Value: bankVal},
				},
			},
		},
	}

	require.NoError(t, flatStore.ApplyChangeSets(changeSets))
	_, err = flatStore.Commit()
	require.NoError(t, err)

	require.NoError(t, memDB.ApplyChangeSets(changeSets))
	_, err = memDB.Commit()
	require.NoError(t, err)

	require.True(t, verifyCrossStore(flatStore, memDir))
}

// TestOpenFlatKVReadOnlySurvivesSourcePrune simulates the pruning race where
// a live node deletes the source snapshot mid-way through the tool's clone.
// The tool is expected to keep working because it hardlinks every file in the
// snapshot directory, preserving the inodes after the source is removed.
func TestOpenFlatKVReadOnlySurvivesSourcePrune(t *testing.T) {
	cfg := flatkvconfig.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), "flatkv")

	store, err := flatkv.NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	_, err = store.LoadVersion(0, false)
	require.NoError(t, err)

	nonceKey := evmNonceKey(0x33)
	nonceVal := nonceBytes(42)
	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: keys.EVMStoreKey,
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{{Key: nonceKey, Value: nonceVal}},
			},
		},
	}))
	_, err = store.Commit()
	require.NoError(t, err)
	require.NoError(t, store.WriteSnapshot(""))

	toolStore, err := openFlatKVReadOnly(cfg.DataDir, 0)
	require.NoError(t, err)
	defer func() { require.NoError(t, toolStore.Close()) }()

	// Simulate the live node's atomicRemoveDir: rename the source snapshot
	// out of the way and then remove it. Because the tool hardlinked every
	// file, it should still be able to read.
	entries, err := os.ReadDir(cfg.DataDir)
	require.NoError(t, err)
	var snapDir string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "working" && e.Name() != "changelog" {
			snapDir = filepath.Join(cfg.DataDir, e.Name())
		}
	}
	require.NotEmpty(t, snapDir, "expected to find a snapshot-* dir")
	trashPath := snapDir + "-removing"
	require.NoError(t, os.Rename(snapDir, trashPath))
	require.NoError(t, os.RemoveAll(trashPath))

	got, found := toolStore.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, found, "tool should still read data after source snapshot is pruned")
	require.Equal(t, nonceVal, got)
}

func evmNonceKey(lastByte byte) []byte {
	key := make([]byte, 20)
	key[19] = lastByte
	return keys.BuildEVMKey(keys.EVMKeyNonce, key)
}

func nonceBytes(v uint64) []byte {
	out := make([]byte, 8)
	binary.BigEndian.PutUint64(out, v)
	return out
}

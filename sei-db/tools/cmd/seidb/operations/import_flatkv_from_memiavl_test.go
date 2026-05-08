package operations

import (
	"context"
	"math"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/stretchr/testify/require"
)

func TestImportMemiavlModulesToFlatKVEncodesEVMValues(t *testing.T) {
	homeDir := t.TempDir()
	addr := addrN(0x42)
	eoaAddr := addrN(0x43)
	contractOnlyAddr := addrN(0x44)
	slot := slotN(0x07)
	codeHash := codeHashOf(0xAB)
	contractOnlyCodeHash := codeHashOf(0xCD)
	bytecode := []byte{0x60, 0x2A, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xF3}
	storageValue := padLeft32(0x2A)
	nonceValue := uint64(7)
	eoaNonceValue := uint64(1)
	legacyKey := append([]byte{0x09}, addr[:]...)
	legacyValue := []byte{0x00, 0x03}

	memStore := newTestMemiavlStore(t, homeDir)
	require.NoError(t, memStore.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			storagePair(addr, slot, 0x2A),
			codePair(addr, bytecode),
			noncePair(addr, nonceValue),
			codeHashPair(addr, codeHash),
			noncePair(eoaAddr, eoaNonceValue),
			codeHashPair(contractOnlyAddr, contractOnlyCodeHash),
			{Key: legacyKey, Value: legacyValue},
		}},
	}}))
	version, err := memStore.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), version)
	require.NoError(t, memStore.Close())

	require.NoError(t, importMemiavlModulesToFlatKV(context.Background(), homeDir, []string{keys.EVMStoreKey}, 0, false))

	flatStore := openImportedFlatKVStore(t, homeDir)
	defer func() { require.NoError(t, flatStore.Close()) }()

	gotStorage, found := flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)))
	require.True(t, found)
	require.Equal(t, storageValue, gotStorage)

	gotCode, found := flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCode, addr[:]))
	require.True(t, found)
	require.Equal(t, bytecode, gotCode)

	gotNonce, found := flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
	require.True(t, found)
	require.Equal(t, nonceBytesBE(nonceValue), gotNonce)

	gotCodeHash, found := flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]))
	require.True(t, found)
	require.Equal(t, codeHash[:], gotCodeHash)

	gotEOANonce, found := flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, eoaAddr[:]))
	require.True(t, found)
	require.Equal(t, nonceBytesBE(eoaNonceValue), gotEOANonce)

	gotContractOnlyCodeHash, found := flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCodeHash, contractOnlyAddr[:]))
	require.True(t, found)
	require.Equal(t, contractOnlyCodeHash[:], gotContractOnlyCodeHash)

	gotLegacy, found := flatStore.Get(keys.EVMStoreKey, legacyKey)
	require.True(t, found)
	require.Equal(t, legacyValue, gotLegacy)
}

func TestImportMemiavlModulesToFlatKVRefusesExistingFlatKVWithoutForce(t *testing.T) {
	homeDir := t.TempDir()
	oldAddr := addrN(0x11)
	newAddr := addrN(0x22)

	memStore := newTestMemiavlStore(t, homeDir)
	require.NoError(t, memStore.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(newAddr, 7),
		}},
	}}))
	_, err := memStore.Commit()
	require.NoError(t, err)
	require.NoError(t, memStore.Close())

	flatStore := newTestFlatKVStoreAtHome(t, homeDir)
	require.NoError(t, flatStore.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(oldAddr, 9),
		}},
	}}))
	_, err = flatStore.Commit()
	require.NoError(t, err)
	require.NoError(t, flatStore.Close())

	err = importMemiavlModulesToFlatKV(context.Background(), homeDir, []string{keys.EVMStoreKey}, 0, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already has committed version")
	require.Contains(t, err.Error(), "--force")

	flatStore = openImportedFlatKVStore(t, homeDir)
	gotOldNonce, found := flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, oldAddr[:]))
	require.True(t, found)
	require.Equal(t, nonceBytesBE(9), gotOldNonce)
	_, found = flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, newAddr[:]))
	require.False(t, found)
	require.NoError(t, flatStore.Close())

	require.NoError(t, importMemiavlModulesToFlatKV(context.Background(), homeDir, []string{keys.EVMStoreKey}, 0, true))

	flatStore = openImportedFlatKVStore(t, homeDir)
	defer func() { require.NoError(t, flatStore.Close()) }()
	_, found = flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, oldAddr[:]))
	require.False(t, found)
	gotNewNonce, found := flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, newAddr[:]))
	require.True(t, found)
	require.Equal(t, nonceBytesBE(7), gotNewNonce)
}

func TestImportMemiavlModulesToFlatKVRejectsOutOfRangeResolvedHeight(t *testing.T) {
	err := importMemiavlModulesToFlatKV(context.Background(), t.TempDir(), []string{keys.EVMStoreKey}, math.MaxUint32+1, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of range")
}

func newTestMemiavlStore(t *testing.T, homeDir string) *memiavl.CommitStore {
	t.Helper()
	cfg := memiavl.DefaultConfig()
	cfg.AsyncCommitBuffer = 0
	store := memiavl.NewCommitStore(homeDir, cfg)
	store.Initialize([]string{keys.EVMStoreKey})
	_, err := store.LoadVersion(0, false)
	require.NoError(t, err)
	return store
}

func openImportedFlatKVStore(t *testing.T, homeDir string) *flatkv.CommitStore {
	t.Helper()
	return newTestFlatKVStoreAtHome(t, homeDir)
}

func newTestFlatKVStoreAtHome(t *testing.T, homeDir string) *flatkv.CommitStore {
	t.Helper()
	cfg := flatkvconfig.DefaultTestConfig(t)
	cfg.DataDir = utils.GetFlatKVPath(homeDir)
	store, err := flatkv.NewCommitStore(context.Background(), cfg)
	require.NoError(t, err)
	_, err = store.LoadVersion(0, false)
	require.NoError(t, err)
	return store
}

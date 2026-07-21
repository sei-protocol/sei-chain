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
	miscKey := append([]byte{0x09}, addr[:]...)
	miscValue := []byte{0x00, 0x03}

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
			{Key: miscKey, Value: miscValue},
		}},
	}}))
	version, err := memStore.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), version)
	require.NoError(t, memStore.Close())

	require.NoError(t, importMemiavlModulesToFlatKV(context.Background(), homeDir, []string{keys.EVMStoreKey}, 0, false))

	flatStore := newTestFlatKVStoreAtHome(t, homeDir)
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

	gotMisc, found := flatStore.Get(keys.EVMStoreKey, miscKey)
	require.True(t, found)
	require.Equal(t, miscValue, gotMisc)
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
	_, err = flatStore.Commit(flatStore.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, flatStore.Close())

	err = importMemiavlModulesToFlatKV(context.Background(), homeDir, []string{keys.EVMStoreKey}, 0, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already has committed version")
	require.Contains(t, err.Error(), "--force")

	flatStore = newTestFlatKVStoreAtHome(t, homeDir)
	gotOldNonce, found := flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, oldAddr[:]))
	require.True(t, found)
	require.Equal(t, nonceBytesBE(9), gotOldNonce)
	_, found = flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, newAddr[:]))
	require.False(t, found)
	require.NoError(t, flatStore.Close())

	require.NoError(t, importMemiavlModulesToFlatKV(context.Background(), homeDir, []string{keys.EVMStoreKey}, 0, true))

	flatStore = newTestFlatKVStoreAtHome(t, homeDir)
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

// TestImportMemiavlModulesToFlatKVRefusesStaleHeight pins the safety
// contract against the silent-cosmos-rollback footgun on the next
// GIGA_STORAGE startup: when memiavl already has versions past H, the CLI
// MUST refuse --height H instead of writing FlatKV at H. If this check is
// ever dropped, CompositeCommitStore.reconcileVersions
// (sei-db/state_db/sc/composite/store.go) will silently roll memiavl back
// to H on the next start, truncating every cosmos block in (H,
// memiavlLatest]. We also assert the error message points operators at
// the correct recovery step ("roll memiavl back first") so the failure
// mode is debuggable from the CLI output alone.
func TestImportMemiavlModulesToFlatKVRefusesStaleHeight(t *testing.T) {
	homeDir := t.TempDir()
	addr := addrN(0x42)

	memStore := newTestMemiavlStore(t, homeDir)
	require.NoError(t, memStore.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addr, 1)}},
	}}))
	v1, err := memStore.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), v1)

	require.NoError(t, memStore.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addr, 2)}},
	}}))
	v2, err := memStore.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(2), v2)
	require.NoError(t, memStore.Close())

	err = importMemiavlModulesToFlatKV(context.Background(), homeDir, []string{keys.EVMStoreKey}, 1, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to import FlatKV at height 1")
	require.Contains(t, err.Error(), "memiavl latest is 2")
	require.Contains(t, err.Error(), "roll memiavl back")

	// FlatKV must remain untouched after the rejection: the operator can
	// rerun (correctly this time) without --force.
	flatStore := newTestFlatKVStoreAtHome(t, homeDir)
	require.Equal(t, int64(0), flatStore.Version(), "flatkv was opened/written despite stale-height rejection")
	require.NoError(t, flatStore.Close())
}

// TestImportMemiavlModulesToFlatKVRefusesFutureHeight covers the opposite
// edge: --height H with H ahead of memiavl latest. The memiavl exporter
// would have errored on its own, but the explicit guard surfaces the
// problem before any flatkv/exporter machinery spins up, with a message
// that names both H and memiavlLatest.
func TestImportMemiavlModulesToFlatKVRefusesFutureHeight(t *testing.T) {
	homeDir := t.TempDir()
	addr := addrN(0x42)

	memStore := newTestMemiavlStore(t, homeDir)
	require.NoError(t, memStore.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addr, 1)}},
	}}))
	_, err := memStore.Commit()
	require.NoError(t, err)
	require.NoError(t, memStore.Close())

	err = importMemiavlModulesToFlatKV(context.Background(), homeDir, []string{keys.EVMStoreKey}, 5, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ahead of memiavl latest 1")
}

// The CLI failure-path contract — that an interrupted import must NOT
// finalize partial state — is locked in at the unit level by the
// TestKVImporter_AbortSkipsFinalize / AbortNilReasonStillAborts /
// AbortAfterCloseIsNoop trio in sei-db/state_db/sc/flatkv/importer_test.go.
// importMemiavlModulesToFlatKV's defer just routes any non-nil return
// through (*flatkv.KVImporter).Abort, which those tests cover directly.
//
// A CLI-level test that exercises this end-to-end (e.g. ctx canceled
// mid-import) trips an unrelated pre-existing race in flatkv.LoadVersion's
// pebble-recovery / dbcache pool interaction; tracking it here would make
// this test brittle and is out of scope for the bug under fix. Once that
// race is addressed, this is a good spot to add an end-to-end variant.

// TestImportMemiavlModulesToFlatKVHandlesLargeDataset exercises the
// memiavl→FlatKV pipeline at a scale large enough to:
//   - cross the importBatchSize threshold inside KVImporter so that
//     dbWorker.flush() fires multiple times instead of just once at Close
//   - exercise dispatcher → worker channel backpressure with a steady
//     stream of pairs across all four FlatKV bucket types
//   - exercise the translator's cross-batch account merge buffer
//     (nonce/codeHash for the same address may land in different
//     translator batches at this volume)
//
// The smaller TestImportMemiavlModulesToFlatKVEncodesEVMValues only writes
// ~7 pairs so the batching/backpressure paths are never hit; this test
// fills that gap without a docker cluster.
//
// Sized to run in a few seconds: ~50K total pairs is enough to trip the
// 20K-pair flush threshold three times on the storage worker while
// keeping CI cost low.
func TestImportMemiavlModulesToFlatKVHandlesLargeDataset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large-dataset import test in -short mode")
	}

	const (
		numAddrs       = 10000
		storagePerAddr = 4
		// totalPairs ≈ numAddrs*(nonce+codeHash+code) + numAddrs*storagePerAddr
		//            = 10000*3 + 10000*4 = 70000 source pairs (storage dominates).
	)
	homeDir := t.TempDir()

	makeAddr := func(i int) ktype.Address {
		var a ktype.Address
		a[16] = byte(i >> 24)
		a[17] = byte(i >> 16)
		a[18] = byte(i >> 8)
		a[19] = byte(i)
		return a
	}
	makeSlot := func(i int) ktype.Slot {
		var s ktype.Slot
		s[28] = byte(i >> 24)
		s[29] = byte(i >> 16)
		s[30] = byte(i >> 8)
		s[31] = byte(i)
		return s
	}

	// Helpers used below force every codeHash and storage value to have a
	// non-zero low byte. flatkv treats an all-zero codeHash or storage value
	// as a tombstone (Get returns false; IsDelete is true), which would
	// silently drop legitimate test fixtures.
	nonzeroByte := func(i int) byte { return byte((i & 0x7F) | 0x80) }

	pairs := make([]*proto.KVPair, 0, numAddrs*(3+storagePerAddr))
	for i := 0; i < numAddrs; i++ {
		addr := makeAddr(i)
		pairs = append(pairs,
			noncePair(addr, uint64(i+1)),
			codeHashPair(addr, codeHashOf(nonzeroByte(i))),
			codePair(addr, []byte{0x60, byte(i & 0xFF), 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xF3}),
		)
		for j := 0; j < storagePerAddr; j++ {
			pairs = append(pairs, storagePair(addr, makeSlot(i*storagePerAddr+j), nonzeroByte(i+j)))
		}
	}

	memStore := newTestMemiavlStore(t, homeDir)
	require.NoError(t, memStore.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: pairs},
	}}))
	version, err := memStore.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), version)
	require.NoError(t, memStore.Close())

	require.NoError(t, importMemiavlModulesToFlatKV(context.Background(), homeDir, []string{keys.EVMStoreKey}, 0, false))

	flatStore := newTestFlatKVStoreAtHome(t, homeDir)
	defer func() { require.NoError(t, flatStore.Close()) }()

	// Spot-check several addresses across the dataset to catch any
	// boundary issues (first / middle / last batch) in the translator's
	// cross-call account merge buffer and the importer's batched writes.
	checkpoints := []int{0, 1, numAddrs / 4, numAddrs / 2, numAddrs - 1}
	for _, i := range checkpoints {
		addr := makeAddr(i)

		gotNonce, found := flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
		require.Truef(t, found, "nonce for addr index %d missing", i)
		require.Equalf(t, nonceBytesBE(uint64(i+1)), gotNonce, "nonce mismatch for addr index %d", i)

		want := codeHashOf(nonzeroByte(i))
		gotCodeHash, found := flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]))
		require.Truef(t, found, "codehash for addr index %d missing", i)
		require.Equalf(t, want[:], gotCodeHash, "codehash mismatch for addr index %d", i)

		expectedCode := []byte{0x60, byte(i & 0xFF), 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xF3}
		gotCode, found := flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCode, addr[:]))
		require.Truef(t, found, "code for addr index %d missing", i)
		require.Equalf(t, expectedCode, gotCode, "code mismatch for addr index %d", i)

		for j := 0; j < storagePerAddr; j++ {
			slot := makeSlot(i*storagePerAddr + j)
			expectedStorage := padLeft32(nonzeroByte(i + j))
			gotStorage, found := flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)))
			require.Truef(t, found, "storage[%d][%d] missing", i, j)
			require.Equalf(t, expectedStorage, gotStorage, "storage[%d][%d] mismatch", i, j)
		}
	}

	// A non-existent address must still miss after the bulk import: this
	// is the regression knob that catches a translator that accidentally
	// emits zero-default rows for unseen account fields when the buffer
	// grows past one batch.
	missingAddr := makeAddr(numAddrs + 1)
	_, found := flatStore.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, missingAddr[:]))
	require.False(t, found, "synthetic-out-of-range address must not exist")
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

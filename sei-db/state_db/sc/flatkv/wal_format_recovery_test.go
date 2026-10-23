package flatkv

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal/statewaltest"
)

// A key read through Get() after recovery, and what it must hold.
type recoveredValue struct {
	// The module the key belongs to.
	module string
	// The logical key.
	key []byte
	// The value Get() must return, or nil if the key must be absent.
	value []byte
}

var (
	recoveryAddressA = addrN(0xa1)
	recoveryAddressB = addrN(0xb2)
	recoverySlot1    = slotN(0x01)
	recoverySlot2    = slotN(0x02)
	recoveryMiscKey  = evmMiscKey(0x01, 0x02, 0x03)
)

// recoveryBlocks returns blocks of changesets that write and delete every kind of row: accounts field by field until
// one is deleted, storage including an all-zero delete, code, EVM misc keys, and a non-EVM module.
func recoveryBlocks() [][]*proto.NamedChangeSet {
	a, b := recoveryAddressA, recoveryAddressB
	balance := func(n byte) []byte {
		value := balanceN(n)
		return value[:]
	}
	codeHash := func(n byte) []byte {
		value := codeHashN(n)
		return value[:]
	}
	evm := func(pairs ...*proto.KVPair) *proto.NamedChangeSet { return namedCS(pairs...) }
	bank := func(pairs ...*proto.KVPair) *proto.NamedChangeSet {
		return &proto.NamedChangeSet{Name: "bank", Changeset: proto.ChangeSet{Pairs: pairs}}
	}
	set := func(key []byte, value []byte) *proto.KVPair { return &proto.KVPair{Key: key, Value: value} }
	del := func(key []byte) *proto.KVPair { return &proto.KVPair{Key: key, Delete: true} }
	nonceKey := func(addr ktype.Address) []byte { return keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]) }
	codeHashKey := func(addr ktype.Address) []byte { return keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]) }
	balanceKey := func(addr ktype.Address) []byte { return keys.BuildEVMKey(keys.EVMKeyBalance, addr[:]) }
	codeKey := func(addr ktype.Address) []byte { return keys.BuildEVMKey(keys.EVMKeyCode, addr[:]) }
	storageKey := func(addr ktype.Address, slot ktype.Slot) []byte {
		return keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	}

	return [][]*proto.NamedChangeSet{
		{
			evm(
				set(nonceKey(a), nonceBytes(1)),
				set(codeHashKey(a), codeHash(0xc1)),
				set(balanceKey(a), balance(10)),
				set(balanceKey(b), balance(20)),
				set(storageKey(a, recoverySlot1), padLeft32(0x11)),
				set(codeKey(a), []byte{0x60, 0x80}),
				set(recoveryMiscKey, []byte("m1")),
			),
			bank(
				set([]byte("supply"), []byte("100")),
				set([]byte("denom"), []byte("usei")),
				set([]byte("empty"), nil),
			),
		},
		{
			evm(
				set(nonceKey(a), nonceBytes(2)),
				set(storageKey(a, recoverySlot1), padLeft32(0x12)),
				set(storageKey(a, recoverySlot2), padLeft32(0x21)),
			),
			bank(set([]byte("supply"), []byte("200"))),
		},
		{
			evm(
				del(balanceKey(b)),
				set(storageKey(a, recoverySlot2), padLeft32()),
				del(recoveryMiscKey),
			),
		},
		{
			evm(del(codeKey(a)), del(codeHashKey(a))),
			bank(del([]byte("empty"))),
		},
		{
			evm(
				set(nonceKey(b), nonceBytes(7)),
				set(balanceKey(b), balance(30)),
				set(storageKey(a, recoverySlot1), padLeft32(0x13)),
			),
		},
		{
			// With its code hash already deleted, deleting the nonce and balance deletes account A's row.
			evm(del(nonceKey(a)), del(balanceKey(a))),
		},
		{
			evm(set(recoveryMiscKey, []byte("m2"))),
			bank(del([]byte("supply"))),
		},
		{
			evm(
				set(storageKey(b, recoverySlot1), padLeft32(0x51)),
				set(codeHashKey(b), codeHash(0xc2)),
			),
		},
	}
}

// expectedRecoveredValues returns what Get() must report after every block of recoveryBlocks() is applied.
func expectedRecoveredValues() []recoveredValue {
	a, b := recoveryAddressA, recoveryAddressB
	balanceB := balanceN(30)
	codeHashB := codeHashN(0xc2)
	return []recoveredValue{
		{module: keys.EVMStoreKey, key: keys.BuildEVMKey(keys.EVMKeyNonce, a[:])},
		{module: keys.EVMStoreKey, key: keys.BuildEVMKey(keys.EVMKeyCodeHash, a[:])},
		{module: keys.EVMStoreKey, key: keys.BuildEVMKey(keys.EVMKeyBalance, a[:])},
		{module: keys.EVMStoreKey, key: keys.BuildEVMKey(keys.EVMKeyCode, a[:])},
		{module: keys.EVMStoreKey, key: keys.BuildEVMKey(keys.EVMKeyNonce, b[:]), value: nonceBytes(7)},
		{module: keys.EVMStoreKey, key: keys.BuildEVMKey(keys.EVMKeyBalance, b[:]), value: balanceB[:]},
		{module: keys.EVMStoreKey, key: keys.BuildEVMKey(keys.EVMKeyCodeHash, b[:]), value: codeHashB[:]},
		{
			module: keys.EVMStoreKey,
			key:    keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(a, recoverySlot1)),
			value:  padLeft32(0x13),
		},
		{
			module: keys.EVMStoreKey,
			key:    keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(a, recoverySlot2)),
		},
		{
			module: keys.EVMStoreKey,
			key:    keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(b, recoverySlot1)),
			value:  padLeft32(0x51),
		},
		{module: keys.EVMStoreKey, key: recoveryMiscKey, value: []byte("m2")},
		{module: "bank", key: []byte("supply")},
		{module: "bank", key: []byte("empty")},
		{module: "bank", key: []byte("denom"), value: []byte("usei")},
	}
}

// What a store that commits recoveryBlocks() directly holds and writes, indexed by height.
type referenceRun struct {
	// The mutations each block's WAL record holds. Index 0 is block 1.
	blockMutations [][]mutation.Mutation

	// The global lattice hash after each height. Index 0 is block 1.
	hashes [][]byte

	// The raw row of every key any block touches, after each height, nil where absent. Index 0 is block 1.
	rows []map[string][]byte
}

// runReference commits blocks to a fresh store, recording the mutations, lattice hash and rows of every height.
func runReference(t *testing.T, blocks [][]*proto.NamedChangeSet) referenceRun {
	t.Helper()
	store := setupTestStoreWithConfig(t, config.DefaultTestConfig(t))
	defer func() { require.NoError(t, store.Close()) }()

	run := referenceRun{
		blockMutations: make([][]mutation.Mutation, len(blocks)),
		hashes:         make([][]byte, len(blocks)),
		rows:           make([]map[string][]byte, len(blocks)),
	}
	touchedKeys := recoveryPhysicalKeys(t, blocks)
	for i, block := range blocks {
		height := int64(i + 1)
		prior := store.OpenView()
		//nolint:gosec // height > 0
		mutations, err := mutation.MutationsFromChangeSets(block, uint64(height), prior)
		prior.Close()
		require.NoError(t, err)
		run.blockMutations[i] = mutations
		require.NoError(t, store.ApplyChangeSets(height, block))
		commitAndCheck(t, store)
		run.hashes[i] = rootHash(store)

		run.rows[i] = make(map[string][]byte, len(touchedKeys))
		view := store.OpenView()
		for _, key := range touchedKeys {
			row, _ := view.GetRaw(key)
			run.rows[i][string(key)] = row
		}
		view.Close()
	}
	return run
}

// recoveryPhysicalKeys returns the physical key of every row blocks touch, each once.
func recoveryPhysicalKeys(t *testing.T, blocks [][]*proto.NamedChangeSet) [][]byte {
	t.Helper()
	seen := make(map[string]bool)
	var physicalKeys [][]byte
	for _, block := range blocks {
		for _, changeSet := range block {
			for _, pair := range changeSet.Changeset.Pairs {
				kind, strippedKey, err := ktype.ParseLogicalKey(changeSet.Name, pair.Key)
				require.NoError(t, err)
				physicalKey := ktype.BuildPhysicalKey(changeSet.Name, pair.Key)
				if kind != keys.EVMKeyMisc {
					physicalKey = ktype.BuildPhysicalEVMKey(kind, strippedKey)
				}
				if !seen[string(physicalKey)] {
					seen[string(physicalKey)] = true
					physicalKeys = append(physicalKeys, physicalKey)
				}
			}
		}
	}
	return physicalKeys
}

// requireMatchesReference requires that store stands at height, with the lattice hash and rows the reference recorded
// for it.
func requireMatchesReference(t *testing.T, store *CommitStore, run referenceRun, height int) {
	t.Helper()
	require.Equal(t, int64(height), store.Version())
	require.Equal(t, run.hashes[height-1], rootHash(store), "height %d", height)
	view := store.OpenView()
	defer view.Close()
	for key, want := range run.rows[height-1] {
		row, found := view.GetRaw([]byte(key))
		require.Equal(t, want != nil, found, "height %d key %x", height, key)
		require.Equal(t, want, row, "height %d key %x", height, key)
	}
}

// TestRecoveryFromWALInEitherBlockFormat rebuilds a store from a WAL whose blocks are in the legacy changeset format,
// in the mutation format, or in both, and requires the recovered state to match a store that committed the same
// blocks directly.
func TestRecoveryFromWALInEitherBlockFormat(t *testing.T) {
	blocks := recoveryBlocks()
	lastBlock := len(blocks)
	run := runReference(t, blocks)

	tests := []struct {
		name string
		// The block the store is snapshotted at before recovery; 0 recovers from genesis.
		snapshotBlock int
		// The first block written as mutations; earlier blocks are written in the legacy format.
		firstMutationBlock int
	}{
		{name: "legacy format from genesis", snapshotBlock: 0, firstMutationBlock: lastBlock + 1},
		{name: "mutation format from genesis", snapshotBlock: 0, firstMutationBlock: 1},
		{name: "both formats from genesis", snapshotBlock: 0, firstMutationBlock: 4},
		{name: "both formats after a snapshot", snapshotBlock: 3, firstMutationBlock: 6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recovered := recoverFromWAL(
				t, blocks, run.blockMutations, tc.snapshotBlock, tc.firstMutationBlock)
			defer func() { require.NoError(t, recovered.Close()) }()

			requireMatchesReference(t, recovered, run, lastBlock)
			for _, expected := range expectedRecoveredValues() {
				value, found := recovered.Get(expected.module, expected.key)
				message := fmt.Sprintf("module %q key %x", expected.module, expected.key)
				require.Equal(t, expected.value != nil, found, message)
				require.Equal(t, expected.value, value, message)
			}
			verifyLtHashConsistency(t, recovered)
		})
	}
}

// TestReadOnlyLoadFromWALInEitherBlockFormat opens read-only stores at heights before, on and after the block where a
// WAL switches from the legacy format to mutations, and requires each to match the reference at its height.
func TestReadOnlyLoadFromWALInEitherBlockFormat(t *testing.T) {
	blocks := recoveryBlocks()
	run := runReference(t, blocks)
	const firstMutationBlock = 4

	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 0
	store := setupTestStoreWithConfig(t, cfg)
	require.NoError(t, store.Close())
	writeMixedWAL(t, cfg.DataDir, blocks, run.blockMutations, firstMutationBlock)

	for _, height := range []int{2, firstMutationBlock, 7} {
		t.Run(fmt.Sprintf("height %d", height), func(t *testing.T) {
			primary, err := newCommitStoreWithWAL(context.Background(), cfg)
			require.NoError(t, err)
			readOnly, err := primary.LoadVersionReadOnly(int64(height))
			require.NoError(t, err)
			require.NoError(t, primary.Close())
			defer func() { require.NoError(t, readOnly.Close()) }()

			requireMatchesReference(t, readOnly.(*CommitStore), run, height)
		})
	}
}

// TestRollbackAcrossWALBlockFormats rolls a store recovered from a WAL in both formats back to a height in the legacy
// format and to one past the switch, and requires each to match the reference at its height.
func TestRollbackAcrossWALBlockFormats(t *testing.T) {
	blocks := recoveryBlocks()
	run := runReference(t, blocks)
	const firstMutationBlock = 6

	for _, height := range []int{4, 7} {
		t.Run(fmt.Sprintf("height %d", height), func(t *testing.T) {
			store := recoverFromWAL(t, blocks, run.blockMutations, 0, firstMutationBlock)
			defer func() { require.NoError(t, store.Close()) }()

			require.NoError(t, store.Rollback(int64(height)))
			requireMatchesReference(t, store, run, height)
			verifyLtHashConsistency(t, store)
		})
	}
}

// recoverFromWAL commits blocks up to snapshotBlock, snapshotting the store there, then replaces its WAL with every
// block, written in the legacy format before firstMutationBlock and as mutations from it on, and reopens the store so
// it replays the blocks after the snapshot.
func recoverFromWAL(
	t *testing.T,
	// The changesets of every block.
	blocks [][]*proto.NamedChangeSet,
	// The mutations of every block.
	blockMutations [][]mutation.Mutation,
	// The block to snapshot the store at before recovery, or 0 for none.
	snapshotBlock int,
	// The first block to write as mutations.
	firstMutationBlock int,
) *CommitStore {
	t.Helper()
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = uint32(snapshotBlock) //nolint:gosec // small test heights
	store := setupTestStoreWithConfig(t, cfg)
	for i := range snapshotBlock {
		require.NoError(t, store.ApplyChangeSets(int64(i+1), blocks[i]))
		commitAndCheck(t, store)
	}
	require.NoError(t, store.Close())
	writeMixedWAL(t, cfg.DataDir, blocks, blockMutations, firstMutationBlock)

	recovered, err := newCommitStoreWithWAL(context.Background(), cfg)
	require.NoError(t, err)
	require.NoError(t, recovered.LoadLatest())
	return recovered
}

// writeMixedWAL replaces the WAL of the closed store in dataDir with every block: in the legacy format before
// firstMutationBlock, and as mutations from it on.
func writeMixedWAL(
	t *testing.T,
	// The data directory of the store.
	dataDir string,
	// The changesets of every block.
	blocks [][]*proto.NamedChangeSet,
	// The mutations of every block.
	blockMutations [][]mutation.Mutation,
	// The first block to write as mutations.
	firstMutationBlock int,
) {
	t.Helper()
	walConfig := StateWALConfig(dataDir)
	require.NoError(t, os.RemoveAll(walConfig.Path))
	legacyBlocks := blocks[:firstMutationBlock-1]
	if len(legacyBlocks) > 0 {
		require.NoError(t, statewaltest.WriteLegacyStateWALBlocks(walConfig.Path, 1, legacyBlocks))
	}
	wal, err := statewal.New(walConfig)
	require.NoError(t, err)
	for block := firstMutationBlock; block <= len(blocks); block++ {
		require.NoError(t, wal.Write(uint64(block), blockMutations[block-1])) //nolint:gosec // block > 0
	}
	require.NoError(t, wal.Close())
}

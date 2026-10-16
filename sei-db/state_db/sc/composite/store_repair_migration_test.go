package composite

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

func repairTestAddr(fill byte) []byte {
	return bytes.Repeat([]byte{fill}, keys.AddressLen)
}

func repairTestSlotKey(slot byte) []byte {
	s := make([]byte, 32)
	s[31] = slot
	return keys.BuildEVMKey(keys.EVMKeyStorage, append(repairTestAddr(0x01), s...))
}

func repairTestWord(v byte) []byte {
	w := make([]byte, 32)
	w[31] = v
	return w
}

// TestComposite_MigrateEVM_RepairWritesSurviveTheMigration applies one block of
// repair-shaped writes in the middle of a migration, to keys on each side of
// the boundary and to keys the same block's batch moves, and checks that every
// key keeps the written value through the end of the migration.
func TestComposite_MigrateEVM_RepairWritesSurviveTheMigration(t *testing.T) {
	dir := t.TempDir()
	addrA, addrB, addrC := repairTestAddr(0xA0), repairTestAddr(0xB0), repairTestAddr(0xC0)
	slot1, slot2, slot3, slot4, slot5 := repairTestSlotKey(1), repairTestSlotKey(2), repairTestSlotKey(3), repairTestSlotKey(4), repairTestSlotKey(5)
	codeC := keys.BuildEVMKey(keys.EVMKeyCode, addrC)
	codeHashA := keys.BuildEVMKey(keys.EVMKeyCodeHash, addrA)
	nonceA := keys.BuildEVMKey(keys.EVMKeyNonce, addrA)
	nonceB := keys.BuildEVMKey(keys.EVMKeyNonce, addrB)
	balanceA := keys.BuildEVMKey(keys.EVMKeyBalance, addrA)
	balanceB := keys.BuildEVMKey(keys.EVMKeyBalance, addrB)

	memCfg := config.DefaultStateCommitConfig()
	memCfg.WriteMode = types.MemiavlOnly
	memCfg.MemIAVLConfig.AsyncCommitBuffer = 0
	cs, err := NewCompositeCommitStore(t.Context(), dir, memCfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	require.NoError(t, cs.LoadLatest())
	// In key order: four storage slots, one code value, one code hash, two
	// nonces, one balance.
	commitEVMPairs(t, cs, []*proto.KVPair{
		{Key: slot1, Value: repairTestWord(0x11)},
		{Key: slot2, Value: repairTestWord(0x12)},
		{Key: slot3, Value: repairTestWord(0x13)},
		{Key: slot4, Value: repairTestWord(0x14)},
		{Key: codeC, Value: []byte{0x60, 0x01}},
		{Key: codeHashA, Value: bytes.Repeat([]byte{0xAB}, 32)},
		{Key: nonceA, Value: []byte{0, 0, 0, 0, 0, 0, 0, 1}},
		{Key: nonceB, Value: []byte{0, 0, 0, 0, 0, 0, 0, 2}},
		{Key: balanceB, Value: repairTestWord(0x05)},
	})
	require.NoError(t, cs.Close())

	cs = reopenInMigrateEVM(t, dir, 3)
	defer func() { _ = cs.Close() }()
	commitEVMPairs(t, cs, nil)

	requireMigrated(t, cs, slot1, slot2, slot3)
	requireNotMigrated(t, cs, slot4, codeC, codeHashA, nonceA, nonceB, balanceB)

	// This block's batch moves slot4, codeC, and codeHashA.
	commitEVMPairs(t, cs, []*proto.KVPair{
		{Key: slot1, Value: repairTestWord(0x91)},
		{Key: slot2, Delete: true},
		{Key: slot5, Value: repairTestWord(0x95)},
		{Key: slot4, Value: repairTestWord(0x94)},
		{Key: codeC, Value: []byte{0x60, 0x02}},
		{Key: codeHashA, Delete: true},
		{Key: nonceA, Value: []byte{0, 0, 0, 0, 0, 0, 0, 9}},
		{Key: balanceA, Value: repairTestWord(0x07)},
		{Key: balanceB, Delete: true},
	})
	requireMigrated(t, cs, slot4, codeC)
	require.False(t, memiavlEVMHolds(cs, codeHashA), "the batch should have moved codeHashA out of memiavl")
	requireNotMigrated(t, cs, nonceA, nonceB)

	want := map[string][]byte{
		string(slot1):     repairTestWord(0x91),
		string(slot2):     nil,
		string(slot3):     repairTestWord(0x13),
		string(slot4):     repairTestWord(0x94),
		string(slot5):     repairTestWord(0x95),
		string(codeC):     {0x60, 0x02},
		string(codeHashA): nil,
		string(nonceA):    {0, 0, 0, 0, 0, 0, 0, 9},
		string(nonceB):    {0, 0, 0, 0, 0, 0, 0, 2},
		string(balanceA):  repairTestWord(0x07),
		string(balanceB):  nil,
	}
	requireEVMValues(t, cs, want)

	for blocks := 0; ; blocks++ {
		done, err := migration.IsAtVersion(flatKVReaderFor(cs), uint64(migration.Version1_MigrateEVM))
		require.NoError(t, err)
		if done {
			break
		}
		require.Less(t, blocks, 20, "the migration did not complete")
		commitEVMPairs(t, cs, nil)
	}
	requireEVMValues(t, cs, want)
	require.False(t, memiavlEVMHoldsAny(cs), "memiavl should hold no EVM keys after the migration")
	require.NoError(t, flatkv.VerifyLtHash(cs.loadFlatKV()))
}

func commitEVMPairs(t *testing.T, cs *CompositeCommitStore, pairs []*proto.KVPair) {
	t.Helper()
	var changesets []*proto.NamedChangeSet
	if len(pairs) > 0 {
		changesets = []*proto.NamedChangeSet{{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: pairs}}}
	}
	require.NoError(t, cs.ApplyChangeSets(changesets))
	_, err := cs.Commit(cs.Version() + 1)
	require.NoError(t, err)
}

func memiavlEVMHolds(cs *CompositeCommitStore, key []byte) bool {
	return cs.memIAVL.GetChildStoreByName(keys.EVMStoreKey).Get(key) != nil
}

func requireMigrated(t *testing.T, cs *CompositeCommitStore, evmKeys ...[]byte) {
	t.Helper()
	for _, key := range evmKeys {
		require.False(t, memiavlEVMHolds(cs, key), "key %x should have left memiavl", key)
		_, ok := cs.loadFlatKV().Get(keys.EVMStoreKey, key)
		require.True(t, ok, "key %x should be in flatkv", key)
	}
}

func requireNotMigrated(t *testing.T, cs *CompositeCommitStore, evmKeys ...[]byte) {
	t.Helper()
	for _, key := range evmKeys {
		require.True(t, memiavlEVMHolds(cs, key), "key %x should still be in memiavl", key)
	}
}

func requireEVMValues(t *testing.T, cs *CompositeCommitStore, want map[string][]byte) {
	t.Helper()
	for key, value := range want {
		got, ok, err := cs.Get(keys.EVMStoreKey, []byte(key))
		require.NoError(t, err)
		if value == nil {
			require.False(t, ok, "key %x should be absent, found %x", key, got)
			continue
		}
		require.True(t, ok, "key %x should be present", key)
		require.Equal(t, value, got, "key %x", key)
	}
}

func memiavlEVMHoldsAny(cs *CompositeCommitStore) bool {
	iter := cs.memIAVL.GetChildStoreByName(keys.EVMStoreKey).Iterator(nil, nil, true)
	defer func() { _ = iter.Close() }()
	return iter.Valid()
}

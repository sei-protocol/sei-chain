package flatkv

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// rawRow is a physical key and the bytes a block is expected to leave under it.
type rawRow struct {
	key   []byte
	value []byte
}

// commitRowInEveryStore commits block 1 with one row in each of FlatKV's stores, and returns the
// physical key and serialized bytes of each.
func commitRowInEveryStore(t *testing.T, s *CommitStore) []rawRow {
	t.Helper()
	addr := addrN(1)
	slot := slotN(2)
	balance := balanceN(3)
	codeHash := codeHashN(4)
	bytecode := []byte{0x60, 0x00}
	evmMisc := evmMiscKey(0x05)
	bankKey := []byte{0x06}

	require.NoError(t, s.CommitStateChanges(1, []*proto.NamedChangeSet{
		namedCS(
			noncePair(addr, 7),
			balancePair(addr, balance),
			codeHashPair(addr, codeHash),
			storagePair(addr, slot, []byte{0x08}),
			codePair(addr, bytecode),
			&proto.KVPair{Key: evmMisc, Value: []byte{0x09}},
		),
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: bankKey, Value: []byte{0x0a}}}}},
	}))

	account := vtype.NewAccountData().SetBlockHeight(1).SetNonce(7).SetBalance(&balance).SetCodeHash(&codeHash)
	storage, err := vtype.SerializeStorage(1, padLeft32(0x08))
	require.NoError(t, err)
	return []rawRow{
		{key: accountPhysKey(addr), value: account.Serialize()},
		{key: storagePhysKey(addr, slot), value: storage},
		{key: ktype.EVMPhysicalKey(keys.EVMKeyCode, addr[:]), value: vtype.SerializeCode(1, bytecode)},
		{key: ktype.ModulePhysicalKey(keys.EVMStoreKey, evmMisc), value: vtype.SerializeMisc(1, []byte{0x09})},
		{key: ktype.ModulePhysicalKey("bank", bankKey), value: vtype.SerializeMisc(1, []byte{0x0a})},
	}
}

// GetRaw answers with the row exactly as stored, metadata included, in every store a key can route to.
func TestGetRawReturnsStoredRows(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	rows := commitRowInEveryStore(t, s)

	stateView := s.OpenView()
	defer stateView.Close()

	for _, row := range rows {
		value, found := stateView.GetRaw(row.key)
		require.True(t, found, "key %q", row.key)
		require.Equal(t, row.value, value, "key %q", row.key)
	}
}

// A physical key that holds nothing is reported as not found, whichever store it routes to.
func TestGetRawMissingKey(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	commitRowInEveryStore(t, s)

	stateView := s.OpenView()
	defer stateView.Close()

	missing := addrN(0x20)
	for _, key := range [][]byte{
		accountPhysKey(missing),
		storagePhysKey(missing, slotN(1)),
		ktype.EVMPhysicalKey(keys.EVMKeyCode, missing[:]),
		ktype.ModulePhysicalKey(keys.EVMStoreKey, evmMiscKey(0x20)),
		ktype.ModulePhysicalKey("bank", []byte{0x20}),
	} {
		value, found := stateView.GetRaw(key)
		require.False(t, found, "key %q", key)
		require.Nil(t, value, "key %q", key)
	}
}

// One batch can span every store and include keys that hold nothing; only the found keys come back.
func TestBatchGetRawMixesStoresAndMissingKeys(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	rows := commitRowInEveryStore(t, s)

	stateView := s.OpenView()
	defer stateView.Close()

	missing := addrN(0x20)
	requested := [][]byte{accountPhysKey(missing), storagePhysKey(missing, slotN(1))}
	expected := map[string][]byte{}
	for _, row := range rows {
		requested = append(requested, row.key)
		expected[string(row.key)] = row.value
	}

	require.Equal(t, expected, stateView.BatchGetRaw(requested))
}

// An empty batch reads nothing and returns an empty map.
func TestBatchGetRawEmpty(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	commitRowInEveryStore(t, s)

	stateView := s.OpenView()
	defer stateView.Close()

	require.Empty(t, stateView.BatchGetRaw(nil))
}

// Raw reads are pinned to the view's block like every other read: a later block's write is invisible
// to a view opened before it, and visible to one opened after.
func TestRawReadsArePinnedToTheViewBlock(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	rows := commitRowInEveryStore(t, s)
	accountRow := rows[0]

	before := s.OpenView()
	defer before.Close()

	addr := addrN(1)
	commitNonce(t, s, 2, addr, 8)

	value, found := before.GetRaw(accountRow.key)
	require.True(t, found)
	require.Equal(t, accountRow.value, value)
	require.Equal(t, map[string][]byte{string(accountRow.key): accountRow.value},
		before.BatchGetRaw([][]byte{accountRow.key}))

	after := s.OpenView()
	defer after.Close()

	balance := balanceN(3)
	codeHash := codeHashN(4)
	updated := vtype.NewAccountData().SetBlockHeight(2).SetNonce(8).SetBalance(&balance).SetCodeHash(&codeHash)
	value, found = after.GetRaw(accountRow.key)
	require.True(t, found)
	require.Equal(t, updated.Serialize(), value)
}

// A key with no module separator cannot be routed to any store.
func TestGetRawRejectsUnroutableKey(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	commitRowInEveryStore(t, s)

	stateView := s.OpenView()
	defer stateView.Close()

	require.Panics(t, func() { stateView.GetRaw([]byte("no-separator")) })
	require.Panics(t, func() { stateView.BatchGetRaw([][]byte{[]byte("no-separator")}) })
}

// A key under the metadata prefix names the store's own bookkeeping, not block state.
func TestGetRawRejectsMetaKey(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	rows := commitRowInEveryStore(t, s)

	stateView := s.OpenView()
	defer stateView.Close()

	metaKey := []byte(ktype.MetaKeyPrefix + "version")
	require.Panics(t, func() { stateView.GetRaw(metaKey) })
	require.Panics(t, func() { stateView.BatchGetRaw([][]byte{rows[0].key, metaKey}) })
}

// storeViewFor reports a key it cannot serve as an error rather than panicking.
func TestStoreViewForRejectsMetaKey(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()
	commitRowInEveryStore(t, s)

	stateView := s.OpenView()
	defer stateView.Close()
	flatView := stateView.(*flatKVStateView)

	for _, key := range [][]byte{[]byte(ktype.MetaKeyPrefix + "version"), []byte("no-separator")} {
		var err error
		require.NotPanics(t, func() { _, err = flatView.storeViewFor(key) })
		require.Error(t, err, "key %q", key)
	}
}

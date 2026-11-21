package flatkv

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
)

// A row view over the live store's own rows answers every read the live store's view does, including
// for accounts and slots a later block deleted and for keys that never existed.
func TestRowViewDecodesRowsAsTheLiveViewDoes(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	alice, bob, carol, contract := addrN(1), addrN(2), addrN(3), addrN(4)
	evmMisc := []byte{0x16, 0x01, 0x02}
	require.NoError(t, commitChangeSets(s, 1, []*proto.NamedChangeSet{
		namedCS(
			noncePair(alice, 3),
			balancePair(alice, balanceN(9)),
			balancePair(bob, balanceN(5)),
			codePair(contract, []byte{0x60, 0x00}),
			codeHashPair(contract, codeHashN(0xc0)),
			storagePair(contract, slotN(1), []byte{0x2a}),
			storagePair(contract, slotN(2), []byte{0x2b}),
			&proto.KVPair{Key: evmMisc, Value: []byte{0x05}},
		),
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("supply"), Value: []byte{7}}}}},
	}))
	require.NoError(t, commitChangeSets(s, 2, []*proto.NamedChangeSet{
		namedCS(balanceDeletePair(bob), storageDeletePair(contract, slotN(1))),
	}))

	live := s.OpenView()
	defer live.Close()
	rows := NewRowView(live.GetBlockHeight(), live, nil)
	defer rows.Close()
	require.Equal(t, live.GetBlockHeight(), rows.GetBlockHeight())

	requireSameGet := func(module string, key []byte) {
		t.Helper()
		wantValue, wantFound := live.Get(module, key)
		gotValue, gotFound := rows.Get(module, key)
		require.Equal(t, wantFound, gotFound, "%s/%x", module, key)
		require.Equal(t, wantValue, gotValue, "%s/%x", module, key)
	}
	for _, addr := range []ktype.Address{alice, bob, carol, contract} {
		a := gigaAddr(addr)
		require.Equal(t, live.AccountExists(a), rows.AccountExists(a), "account %x", addr)
		require.Equal(t, live.GetNonce(a), rows.GetNonce(a), "nonce %x", addr)
		require.Equal(t, live.GetBalance(a), rows.GetBalance(a), "balance %x", addr)
		require.Equal(t, live.GetCodeHash(a), rows.GetCodeHash(a), "code hash %x", addr)
		require.Equal(t, live.GetCode(a), rows.GetCode(a), "code %x", addr)
		require.Equal(t, live.GetCodeSize(a), rows.GetCodeSize(a), "code size %x", addr)
		wantAccount, wantExists := live.ReadAccount(a)
		gotAccount, gotExists := rows.ReadAccount(a)
		require.Equal(t, wantExists, gotExists, "account %x", addr)
		require.Equal(t, wantAccount, gotAccount, "account %x", addr)
		for _, kind := range []keys.EVMKeyKind{keys.EVMKeyNonce, keys.EVMKeyBalance, keys.EVMKeyCodeHash, keys.EVMKeyCode} {
			requireSameGet(keys.EVMStoreKey, keys.BuildEVMKey(kind, addr[:]))
		}
		for _, slot := range []ktype.Slot{slotN(1), slotN(2), slotN(3)} {
			require.Equal(t, live.GetStorage(a, gigatypes.Hash(slot)), rows.GetStorage(a, gigatypes.Hash(slot)),
				"storage %x/%x", addr, slot)
			requireSameGet(keys.EVMStoreKey, evmStorageKey(addr, slot))
		}
	}
	requireSameGet(keys.EVMStoreKey, evmMisc)
	requireSameGet(keys.EVMStoreKey, []byte{0x16, 0x09})
	requireSameGet("bank", []byte("supply"))
	requireSameGet("bank", []byte("absent"))

	physical := [][]byte{accountPhysKey(alice), accountPhysKey(carol), storagePhysKey(contract, slotN(2))}
	require.Equal(t, live.BatchGetRaw(physical), rows.BatchGetRaw(physical))
	for _, key := range physical {
		wantValue, wantFound := live.GetRaw(key)
		gotValue, gotFound := rows.GetRaw(key)
		require.Equal(t, wantFound, gotFound, "%x", key)
		require.Equal(t, wantValue, gotValue, "%x", key)
	}
}

// rowMap is a RowReader over fixed rows.
type rowMap map[string][]byte

func (m rowMap) GetRaw(key []byte) ([]byte, bool) {
	value, found := m[string(key)]
	return value, found
}

func TestRowViewReleasesOnce(t *testing.T) {
	releases := 0
	view := NewRowView(3, rowMap{}, func() { releases++ })
	view.Close()
	view.Close()
	require.Equal(t, 1, releases)

	NewRowView(3, rowMap{}, nil).Close()
}

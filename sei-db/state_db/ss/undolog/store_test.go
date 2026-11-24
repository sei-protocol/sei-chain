package undolog

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/stateview"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// fakeLiveView is the live state store's view of one block: its rows by physical key. Methods it does not override
// panic.
type fakeLiveView struct {
	gigatypes.StateView
	rows map[string][]byte
}

func (v fakeLiveView) GetRaw(key []byte) ([]byte, bool) {
	value, found := v.rows[string(key)]
	return value, found
}

func (fakeLiveView) Close() {}

// accountRow returns the live state store's row for an account with balance and nonce.
func accountRow(balance byte, nonce uint64) []byte {
	var b vtype.Balance
	b[len(b)-1] = balance
	return vtype.NewAccountData().SetBalance(&b).SetNonce(nonce).Serialize()
}

func TestUndoLogServesEveryModuleAsOfEachHeight(t *testing.T) {
	addr := gigatypes.Address{0x42}
	accountKey := ktype.BuildPhysicalEVMKey(ktype.EVMKeyAccount, addr[:])
	miscKey := ktype.BuildPhysicalKey("bank", []byte("supply"))
	account1, account2 := accountRow(1, 1), accountRow(2, 2)
	supply1 := []byte("supply-1")

	log, err := Open(t.TempDir(), config.DefaultStateStoreConfig())
	require.NoError(t, err)
	defer func() { require.NoError(t, log.Close()) }()
	require.NoError(t, log.Resume(0, fakeLiveView{rows: map[string][]byte{}}))
	log.ApplyBlock(1, []mutation.Mutation{
		{Key: accountKey, NewValue: account1},
		{Key: miscKey, NewValue: supply1},
	}, fakeLiveView{rows: map[string][]byte{string(accountKey): account1, string(miscKey): supply1}})
	log.ApplyBlock(2, []mutation.Mutation{
		{Key: accountKey, PreviousValue: account1, NewValue: account2},
		{Key: miscKey, PreviousValue: supply1},
	}, fakeLiveView{rows: map[string][]byte{string(accountKey): account2}})
	log.WaitForPendingWrites()

	for height, want := range []struct{ account, misc []byte }{{nil, nil}, {account1, supply1}, {account2, nil}} {
		view, ok := log.OpenView(int64(height))
		require.True(t, ok, "height %d", height)
		account, found := view.GetRaw(accountKey)
		require.Equal(t, want.account != nil, found, "height %d", height)
		require.Equal(t, want.account, account, "height %d", height)
		misc, found := view.GetRaw(miscKey)
		require.Equal(t, want.misc != nil, found, "height %d", height)
		require.Equal(t, want.misc, misc, "height %d", height)
		view.Close()
	}

	// A past row decodes as the live state store's does.
	view, ok := log.OpenView(1)
	require.True(t, ok)
	defer view.Close()
	account, exists := stateview.New(view).ReadAccount(addr)
	require.True(t, exists)
	require.Equal(t, gigatypes.Account{Balance: common.Hash{31: 1}, Nonce: 1, CodeHash: gigatypes.EmptyCodeHash}, account)
}

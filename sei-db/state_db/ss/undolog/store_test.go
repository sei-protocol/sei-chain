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

// fakeLiveView is the live state store's view of one block: its values by physical key. Methods it does not
// override panic.
type fakeLiveView struct {
	gigatypes.StateView
	values map[string][]byte
}

func (v fakeLiveView) GetRaw(key []byte) ([]byte, bool) {
	value, found := v.values[string(key)]
	return value, found
}

func (fakeLiveView) Close() {}

// accountValue returns the live state store's stored value for an account with balance and nonce.
func accountValue(balance byte, nonce uint64) []byte {
	var b vtype.Balance
	b[len(b)-1] = balance
	return vtype.NewAccountData().SetBalance(&b).SetNonce(nonce).Serialize()
}

func TestUndoLogServesEveryModuleAsOfEachHeight(t *testing.T) {
	addr := gigatypes.Address{0x42}
	accountKey := ktype.BuildPhysicalEVMKey(ktype.EVMKeyAccount, addr[:])
	miscKey := ktype.BuildPhysicalKey("bank", []byte("supply"))
	account1, account2 := accountValue(1, 1), accountValue(2, 2)
	supply1 := []byte("supply-1")

	log, err := Open(t.TempDir(), config.DefaultStateStoreConfig())
	require.NoError(t, err)
	defer func() { require.NoError(t, log.Close()) }()
	require.NoError(t, log.Resume(0, fakeLiveView{values: map[string][]byte{}}))
	log.ApplyBlock(1, []mutation.Mutation{
		{Key: accountKey, NewValue: account1},
		{Key: miscKey, NewValue: supply1},
	}, fakeLiveView{values: map[string][]byte{string(accountKey): account1, string(miscKey): supply1}})
	log.ApplyBlock(2, []mutation.Mutation{
		{Key: accountKey, PreviousValue: account1, NewValue: account2},
		{Key: miscKey, PreviousValue: supply1},
	}, fakeLiveView{values: map[string][]byte{string(accountKey): account2}})
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

	// A past value decodes as the live state store's does.
	view, ok := log.OpenView(1)
	require.True(t, ok)
	defer view.Close()
	account, exists := stateview.New(view).ReadAccount(addr)
	require.True(t, exists)
	require.Equal(t, gigatypes.Account{Balance: common.Hash{31: 1}, Nonce: 1, CodeHash: gigatypes.EmptyCodeHash}, account)
}

func TestApplyBlockRefusesAMutationTheLiveStateStoreCouldNotCommit(t *testing.T) {
	addr := gigatypes.Address{0x42}
	accountKey := ktype.BuildPhysicalEVMKey(ktype.EVMKeyAccount, addr[:])
	log, err := Open(t.TempDir(), config.DefaultStateStoreConfig())
	require.NoError(t, err)
	defer func() { require.NoError(t, log.Close()) }()
	require.NoError(t, log.Resume(0, fakeLiveView{values: map[string][]byte{}}))

	require.Panics(t, func() {
		log.ApplyBlock(1, []mutation.Mutation{
			{Key: accountKey, PreviousValue: make([]byte, vtype.BalanceLength), NewValue: accountValue(1, 1)},
		}, fakeLiveView{values: map[string][]byte{string(accountKey): accountValue(1, 1)}})
	})
	log.WaitForPendingWrites()
	require.Equal(t, int64(0), log.GetLatestVersion())
}

func TestReplayBlockRefusesAMutationTheLiveStateStoreCouldNotCommit(t *testing.T) {
	addr := gigatypes.Address{0x42}
	accountKey := ktype.BuildPhysicalEVMKey(ktype.EVMKeyAccount, addr[:])
	log, err := Open(t.TempDir(), config.DefaultStateStoreConfig())
	require.NoError(t, err)
	defer func() { require.NoError(t, log.Close()) }()

	err = log.ReplayBlock(1, []mutation.Mutation{
		{Key: accountKey, PreviousValue: make([]byte, vtype.BalanceLength), NewValue: accountValue(1, 1)},
	})
	require.ErrorContains(t, err, "account value")
	require.Equal(t, int64(0), log.GetLatestVersion())

	require.NoError(t, log.ReplayBlock(1, []mutation.Mutation{{Key: accountKey, NewValue: accountValue(1, 1)}}))
	require.Equal(t, int64(1), log.GetLatestVersion())
}

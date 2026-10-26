package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

func TestFlatKVMemoryStoreServesCommittedRowsByHeight(t *testing.T) {
	store := NewFlatKVMemoryStore()
	encode := NewFlatKVChangeSetEncoder()
	account := common.HexToAddress("0x1000000000000000000000000000000000000001")
	contract := common.HexToAddress("0x2000000000000000000000000000000000000002")
	slotA := common.HexToHash("0xa")
	slotB := common.HexToHash("0xb")

	commit := func(height int64, changes StateChangeSet) {
		t.Helper()
		changesets, err := encode(changes)
		require.NoError(t, err)
		require.NoError(t, store.CommitStateChanges(height, changesets))
	}
	commit(1, StateChangeSet{
		Balances: []BalanceChange{{Address: account, Balance: big.NewInt(100)}},
		Nonces:   []NonceChange{{Address: account, Nonce: 3}},
		Code:     []CodeChange{{Address: contract, Code: []byte{0x60, 0x00}}},
		Storage: []StorageChange{
			{Address: contract, Key: slotA, Value: common.HexToHash("0x1")},
			{Address: contract, Key: slotB, Value: common.HexToHash("0x2")},
		},
	})
	first := store.OpenView()
	commit(2, StateChangeSet{
		Balances: []BalanceChange{{Address: account, Balance: big.NewInt(7)}},
		Storage: []StorageChange{
			{Address: contract, Key: slotA, Delete: true},
			{Address: contract, Key: slotB, Value: common.HexToHash("0x9")},
		},
	})
	second := store.OpenView()

	require.Equal(t, common.BigToHash(big.NewInt(100)), first.GetBalance(account))
	require.Equal(t, common.HexToHash("0x1"), first.GetStorage(contract, slotA))
	require.Equal(t, common.BigToHash(big.NewInt(7)), second.GetBalance(account))
	require.Equal(t, uint64(3), second.GetNonce(account))
	require.Equal(t, gigatypes.EmptyCodeHash, second.GetCodeHash(account))
	require.Equal(t, common.Hash{}, second.GetStorage(contract, slotA))
	require.Equal(t, common.HexToHash("0x9"), second.GetStorage(contract, slotB))
	require.Equal(t, []byte{0x60, 0x00}, second.GetCode(contract))
	require.False(t, second.AccountExists(common.HexToAddress("0x3")))
	atFirst, ok := store.OpenViewAt(1)
	require.True(t, ok)
	require.Equal(t, common.BigToHash(big.NewInt(100)), atFirst.GetBalance(account))

	require.Error(t, store.CommitStateChanges(4, nil))
}

func TestFlatKVMemoryStoreSeedsInitialVersionAndKeepsModuleRows(t *testing.T) {
	store := NewFlatKVMemoryStore()
	require.NoError(t, store.SetInitialVersion(5))
	latest, err := store.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(4), latest)

	require.NoError(t, store.CommitStateChanges(5, []*proto.NamedChangeSet{{
		Name:      "module",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("key"), Value: []byte("value")}}},
	}}))
	latest, err = store.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(5), latest)
	value, ok := store.Get("module", []byte("key"))
	require.True(t, ok)
	require.Equal(t, []byte("value"), value)
	require.Error(t, store.SetInitialVersion(10))
}

func TestQueuedMemoryReceiptStoreReturnsMarshaledReceipts(t *testing.T) {
	store := NewQueuedMemoryReceiptStore()
	hash := common.HexToHash("0xabc")
	stored := &evmtypes.Receipt{TxHashHex: hash.Hex(), BlockNumber: 2, GasUsed: 21000, Status: 1}
	ctx := sdk.NewContext(nil, tmproto.Header{Height: 2}, false)
	require.NoError(t, store.SetReceipts(ctx, []receipt.ReceiptRecord{{TxHash: hash, Receipt: stored}}))
	require.NoError(t, store.Close())

	got, err := store.GetReceipt(ctx, hash)
	require.NoError(t, err)
	require.Equal(t, stored.GasUsed, got.GasUsed)
	require.Equal(t, stored.TxHashHex, got.TxHashHex)
	require.Equal(t, int64(2), store.LatestVersion())
}

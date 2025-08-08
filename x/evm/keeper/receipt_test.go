package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func setupKeeper(t *testing.T) (sdk.Context, TestKeeper) {
	// TODO: implement a real setup or mock if needed
	t.Helper()
	return sdk.Context{}, TestKeeper{}
}

type TestKeeper struct{}

func (k TestKeeper) StoreReceipt(ctx sdk.Context, receipt *types.Receipt) {
	// stub - test logic or mock goes here
}

func (k TestKeeper) StoreKey() sdk.StoreKey {
	return sdk.NewKVStoreKey("evm")
}

func TestStoreReceipt(t *testing.T) {
	ctx, keeper := setupKeeper(t)
	receipt := &types.Receipt{TxHash: []byte("abc123")}

	keeper.StoreReceipt(ctx, receipt)
	store := ctx.KVStore(keeper.StoreKey())
	bz := store.Get(types.GetReceiptKey(receipt.TxHash))

	require.NotNil(t, bz)
}

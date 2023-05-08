package dex_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestTx struct {
	msgs []sdk.Msg
	gas  uint64
	fee  sdk.Coins
}

func (tx TestTx) GetMsgs() []sdk.Msg {
	return tx.msgs
}

func (tx TestTx) ValidateBasic() error {
	return nil
}

func (tx TestTx) GetGas() uint64 {
	return tx.gas
}
func (tx TestTx) GetFee() sdk.Coins {
	return tx.fee
}
func (tx TestTx) FeePayer() sdk.AccAddress {
	return nil
}
func (tx TestTx) FeeGranter() sdk.AccAddress {
	return nil
}

func TestIsDecimalMultipleOf(t *testing.T) {
	v1, _ := sdk.NewDecFromStr("2.4")
	v2, _ := sdk.NewDecFromStr("1.2")
	v3, _ := sdk.NewDecFromStr("2")
	v4, _ := sdk.NewDecFromStr("100.5")
	v5, _ := sdk.NewDecFromStr("0.5")
	v6, _ := sdk.NewDecFromStr("1.5")
	v7, _ := sdk.NewDecFromStr("1.01")
	v8, _ := sdk.NewDecFromStr("3")
	v9, _ := sdk.NewDecFromStr("5.4")
	v10, _ := sdk.NewDecFromStr("0.3")
	v11 := sdk.ZeroDec()

	assert.True(t, dex.IsDecimalMultipleOf(v1, v2))
	assert.True(t, !dex.IsDecimalMultipleOf(v2, v1))
	assert.True(t, !dex.IsDecimalMultipleOf(v3, v2))
	assert.True(t, dex.IsDecimalMultipleOf(v3, v5))
	assert.True(t, !dex.IsDecimalMultipleOf(v3, v6))
	assert.True(t, dex.IsDecimalMultipleOf(v4, v5))
	assert.True(t, !dex.IsDecimalMultipleOf(v2, v1))
	assert.True(t, dex.IsDecimalMultipleOf(v6, v5))
	assert.True(t, !dex.IsDecimalMultipleOf(v7, v3))
	assert.True(t, dex.IsDecimalMultipleOf(v8, v6))
	assert.True(t, dex.IsDecimalMultipleOf(v9, v10))

	// Zero is multiple of all
	assert.True(t, dex.IsDecimalMultipleOf(v11, v1))
	assert.True(t, dex.IsDecimalMultipleOf(v11, v2))
	assert.True(t, dex.IsDecimalMultipleOf(v11, v3))
	assert.True(t, dex.IsDecimalMultipleOf(v11, v4))
	assert.True(t, dex.IsDecimalMultipleOf(v11, v5))
	assert.True(t, dex.IsDecimalMultipleOf(v11, v6))
	assert.True(t, dex.IsDecimalMultipleOf(v11, v7))
	assert.True(t, dex.IsDecimalMultipleOf(v11, v8))
	assert.True(t, dex.IsDecimalMultipleOf(v11, v9))
	assert.True(t, dex.IsDecimalMultipleOf(v11, v10))
	assert.True(t, dex.IsDecimalMultipleOf(v11, v11))
}

func TestCheckDexGasDecorator(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	decorator := dex.NewCheckDexGasDecorator(*keeper)
	terminator := func(ctx sdk.Context, tx sdk.Tx, simulate bool) (newCtx sdk.Context, err error) { return ctx, nil }
	tx := TestTx{
		msgs: []sdk.Msg{
			types.NewMsgPlaceOrders("someone", []*types.Order{{}, {}}, keepertest.TestContract, sdk.NewCoins()),
			types.NewMsgCancelOrders("someone", []*types.Cancellation{{}, {}, {}}, keepertest.TestContract),
		},
		fee: sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(27500))),
	}
	_, err := decorator.AnteHandle(ctx, tx, false, terminator)
	require.Nil(t, err)
	tx = TestTx{
		msgs: []sdk.Msg{
			types.NewMsgPlaceOrders("someone", []*types.Order{{}, {}}, keepertest.TestContract, sdk.NewCoins()),
			types.NewMsgCancelOrders("someone", []*types.Cancellation{{}, {}, {}}, keepertest.TestContract),
		},
		fee: sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(25499))),
	}
	_, err = decorator.AnteHandle(ctx, tx, false, terminator)
	require.NotNil(t, err)
	tx = TestTx{
		msgs: []sdk.Msg{
			types.NewMsgPlaceOrders("someone", []*types.Order{{}}, keepertest.TestContract, sdk.NewCoins()),
		},
	}
	_, err = decorator.AnteHandle(ctx, tx, false, terminator)
	require.NotNil(t, err)
	tx = TestTx{
		msgs: []sdk.Msg{},
	}
	_, err = decorator.AnteHandle(ctx, tx, false, terminator)
	require.Nil(t, err)
	tx = TestTx{
		msgs: []sdk.Msg{types.NewMsgContractDepositRent(keepertest.TestContract, 10, keepertest.TestAccount)},
	}
	_, err = decorator.AnteHandle(ctx, tx, false, terminator)
	require.Nil(t, err)
}

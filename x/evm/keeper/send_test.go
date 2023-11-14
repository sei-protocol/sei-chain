package keeper_test

import (
	"math"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestEVMToEVMSend(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, evmAddr1 := testkeeper.MockAddressPair()
	_, evmAddr2 := testkeeper.MockAddressPair()
	require.NotNil(t, k.EVMToEVMSend(ctx, evmAddr1, evmAddr2, 10))
	require.Nil(t, k.CreditAddress(ctx, evmAddr1, 20))
	require.Nil(t, k.EVMToEVMSend(ctx, evmAddr1, evmAddr2, 10))
	require.Equal(t, uint64(10), k.GetBalance(ctx, evmAddr1))
	require.Equal(t, uint64(10), k.GetBalance(ctx, evmAddr2))
}

func TestEVMToBankSend(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, evmAddr1 := testkeeper.MockAddressPair()
	seiAddr2, _ := testkeeper.MockAddressPair()
	require.NotNil(t, k.EVMToBankSend(ctx, evmAddr1, seiAddr2, 10))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(20))))
	require.Nil(t, k.CreditAddress(ctx, evmAddr1, 20))
	require.Nil(t, k.EVMToBankSend(ctx, evmAddr1, seiAddr2, 10))
	require.Equal(t, uint64(10), k.GetBalance(ctx, evmAddr1))
	require.Equal(t, int64(10), k.BankKeeper().GetBalance(ctx, seiAddr2, "usei").Amount.Int64())
}

func TestBankToEVMSend(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	seiAddr1, _ := testkeeper.MockAddressPair()
	_, evmAddr2 := testkeeper.MockAddressPair()
	require.NotNil(t, k.BankToEVMSend(ctx, seiAddr1, evmAddr2, 10))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(20))))
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, seiAddr1, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(20))))
	require.Nil(t, k.BankToEVMSend(ctx, seiAddr1, evmAddr2, 10))
	require.Equal(t, uint64(10), k.GetBalance(ctx, evmAddr2))
	require.Equal(t, int64(10), k.BankKeeper().GetBalance(ctx, seiAddr1, "usei").Amount.Int64())
}

func TestCreditAddress(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, evmAddr1 := testkeeper.MockAddressPair()
	require.Nil(t, k.CreditAddress(ctx, evmAddr1, math.MaxUint64))
	require.NotNil(t, k.CreditAddress(ctx, evmAddr1, 1))
}

func TestDebitAddress(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, evmAddr1 := testkeeper.MockAddressPair()
	require.NotNil(t, k.DebitAddress(ctx, evmAddr1, 1))
}

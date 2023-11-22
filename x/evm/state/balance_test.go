package state_test

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestAddBalance(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(15)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, state.GetMiddleManAddress(ctx), amt)
	db := state.NewDBImpl(ctx, k, false)
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	db.AddBalance(evmAddr, big.NewInt(0))

	// set association
	k.SetAddressMapping(db.Ctx(), seiAddr, evmAddr)
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	db.AddBalance(evmAddr, big.NewInt(10000000000000))
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(10000000000000))

	_, evmAddr2 := testkeeper.MockAddressPair()
	db.SubBalance(evmAddr2, big.NewInt(-5000000000000)) // should redirect to AddBalance
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(10000000000000))
	require.Equal(t, db.GetBalance(evmAddr2), big.NewInt(5000000000000))
}

func TestSubBalance(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	db := state.NewDBImpl(ctx, k, false)
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	db.SubBalance(evmAddr, big.NewInt(0))

	// set association
	k.SetAddressMapping(db.Ctx(), seiAddr, evmAddr)
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(20)))
	k.BankKeeper().MintCoins(db.Ctx(), types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(db.Ctx(), types.ModuleName, seiAddr, amt)
	db.SubBalance(evmAddr, big.NewInt(10000000000000))
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(10000000000000))

	_, evmAddr2 := testkeeper.MockAddressPair()
	amt = sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(10)))
	k.BankKeeper().MintCoins(db.Ctx(), types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(db.Ctx(), types.ModuleName, sdk.AccAddress(evmAddr2[:]), amt)
	db.AddBalance(evmAddr2, big.NewInt(-5000000000000)) // should redirect to SubBalance
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(10000000000000))
	require.Equal(t, db.GetBalance(evmAddr2), big.NewInt(5000000000000))

	// insufficient balance
	db.SubBalance(evmAddr2, big.NewInt(10000000000000))
	require.NotNil(t, db.Err())
}

func TestSetBalance(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	db := state.NewDBImpl(ctx, k, true)
	_, evmAddr := testkeeper.MockAddressPair()
	db.SetBalance(evmAddr, big.NewInt(10000000000000))
	require.Equal(t, big.NewInt(10000000000000), db.GetBalance(evmAddr))

	seiAddr2, evmAddr2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(db.Ctx(), seiAddr2, evmAddr2)
	db.SetBalance(evmAddr2, big.NewInt(10000000000000))
	require.Equal(t, big.NewInt(10000000000000), db.GetBalance(evmAddr2))
}

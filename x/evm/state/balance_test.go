package state_test

import (
	"errors"
	"math"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestAddBalance(t *testing.T) {
	k, _, ctx := keeper.MockEVMKeeper()
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(15)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, amt)
	db := state.NewDBImpl(ctx, k)
	seiAddr, evmAddr := keeper.MockAddressPair()
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	db.AddBalance(evmAddr, big.NewInt(0))

	// set association
	k.SetAddressMapping(db.Ctx(), seiAddr, evmAddr)
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	db.AddBalance(evmAddr, big.NewInt(10))
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(10))

	_, evmAddr2 := keeper.MockAddressPair()
	db.SubBalance(evmAddr2, big.NewInt(-5)) // should redirect to AddBalance
	require.Nil(t, db.Err())
	// minted should not increase because the account is not associated
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(10))
	require.Equal(t, db.GetBalance(evmAddr2), big.NewInt(5))

	// overflow
	db.AddBalance(evmAddr2, big.NewInt(math.MaxInt64))
	db.AddBalance(evmAddr2, big.NewInt(math.MaxInt64))
	require.NotNil(t, db.Err())
}

func TestSubBalance(t *testing.T) {
	k, _, ctx := keeper.MockEVMKeeper()
	db := state.NewDBImpl(ctx, k)
	seiAddr, evmAddr := keeper.MockAddressPair()
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	db.SubBalance(evmAddr, big.NewInt(0))

	// set association
	k.SetAddressMapping(db.Ctx(), seiAddr, evmAddr)
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(20)))
	k.BankKeeper().MintCoins(db.Ctx(), types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(db.Ctx(), types.ModuleName, seiAddr, amt)
	db.SubBalance(evmAddr, big.NewInt(10))
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(10))

	_, evmAddr2 := keeper.MockAddressPair()
	k.SetOrDeleteBalance(db.Ctx(), evmAddr2, 10)
	db.AddBalance(evmAddr2, big.NewInt(-5)) // should redirect to SubBalance
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(10))
	require.Equal(t, db.GetBalance(evmAddr2), big.NewInt(5))

	// insufficient balance
	db.SubBalance(evmAddr2, big.NewInt(10))
	require.NotNil(t, db.Err())
}

func TestCheckBalance(t *testing.T) {
	k, _, ctx := keeper.MockEVMKeeper()
	db := state.NewDBImpl(ctx, k)
	require.Nil(t, db.CheckBalance())

	db.WithErr(errors.New("test"))
	require.NotNil(t, db.CheckBalance())
	db.WithErr(nil)

	// subbalance with unassociated address
	k, _, ctx = keeper.MockEVMKeeper()
	_, evmAddr := keeper.MockAddressPair()
	k.SetOrDeleteBalance(ctx, evmAddr, 1000)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, amt)
	db = state.NewDBImpl(ctx, k)
	db.SubBalance(evmAddr, big.NewInt(500))
	require.Nil(t, db.Finalize())
	require.Equal(t, uint64(500), k.GetBalance(ctx, evmAddr))
	require.Equal(t, uint64(500), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), k.GetBaseDenom(ctx)).Amount.Uint64())

	// subbalance with associated address
	k, _, ctx = keeper.MockEVMKeeper()
	seiAddr, evmAddr := keeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	amt = sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, seiAddr, amt)
	db = state.NewDBImpl(ctx, k)
	db.SubBalance(evmAddr, big.NewInt(500))
	require.Nil(t, db.Finalize())
	require.Equal(t, uint64(500), k.BankKeeper().GetBalance(ctx, seiAddr, k.GetBaseDenom(ctx)).Amount.Uint64())
	require.Equal(t, uint64(0), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), k.GetBaseDenom(ctx)).Amount.Uint64())

	// addbalance with unassociated address (should fail since it tries to create tokens from thin air)
	k, _, ctx = keeper.MockEVMKeeper()
	_, evmAddr = keeper.MockAddressPair()
	k.SetOrDeleteBalance(ctx, evmAddr, 1000)
	amt = sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, amt)
	db = state.NewDBImpl(ctx, k)
	db.AddBalance(evmAddr, big.NewInt(500))
	require.NotNil(t, db.Finalize())
	require.Equal(t, uint64(1000), k.GetBalance(ctx, evmAddr))                                                                                                // should remain unchanged
	require.Equal(t, uint64(1000), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), k.GetBaseDenom(ctx)).Amount.Uint64()) // should remain unchanged

	// addbalance with associated address (should fail since it tries to create tokens from thin air)
	k, _, ctx = keeper.MockEVMKeeper()
	seiAddr, evmAddr = keeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	amt = sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, seiAddr, amt)
	db = state.NewDBImpl(ctx, k)
	db.AddBalance(evmAddr, big.NewInt(500))
	require.NotNil(t, db.Finalize())
	require.Equal(t, uint64(1000), k.BankKeeper().GetBalance(ctx, seiAddr, k.GetBaseDenom(ctx)).Amount.Uint64())
	require.Equal(t, uint64(0), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), k.GetBaseDenom(ctx)).Amount.Uint64())
}

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
	db := state.NewStateDBImpl(ctx, k)
	seiAddr, evmAddr := keeper.MockAddressPair()
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	db.AddBalance(evmAddr, big.NewInt(0))

	// set association
	k.SetAddressMapping(db.Ctx(), seiAddr, evmAddr)
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	db.AddBalance(evmAddr, big.NewInt(10))
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBigIntTransientModuleState(state.DeficitKey), big.NewInt(10))
	require.Equal(t, db.GetBigIntTransientModuleState(state.MintedKey), big.NewInt(10))
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(10))

	_, evmAddr2 := keeper.MockAddressPair()
	db.SubBalance(evmAddr2, big.NewInt(-5)) // should redirect to AddBalance
	require.Nil(t, db.Err())
	// minted should not increase because the account is not associated
	require.Equal(t, db.GetBigIntTransientModuleState(state.MintedKey), big.NewInt(10))
	require.Equal(t, db.GetBigIntTransientModuleState(state.DeficitKey), big.NewInt(10))
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(10))
	require.Equal(t, db.GetBalance(evmAddr2), big.NewInt(5))

	// overflow
	db.AddBalance(evmAddr2, big.NewInt(math.MaxInt64))
	db.AddBalance(evmAddr2, big.NewInt(math.MaxInt64))
	require.NotNil(t, db.Err())
}

func TestSubBalance(t *testing.T) {
	k, _, ctx := keeper.MockEVMKeeper()
	db := state.NewStateDBImpl(ctx, k)
	seiAddr, evmAddr := keeper.MockAddressPair()
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	db.SubBalance(evmAddr, big.NewInt(0))

	// set association
	k.SetAddressMapping(db.Ctx(), seiAddr, evmAddr)
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(15)))
	k.BankKeeper().MintCoins(db.Ctx(), types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(db.Ctx(), types.ModuleName, seiAddr, amt)
	db.SubBalance(evmAddr, big.NewInt(10))
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBigIntTransientModuleState(state.DeficitKey), big.NewInt(-10))
	require.Equal(t, db.GetBigIntTransientModuleState(state.MintedKey), big.NewInt(0))
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(5))

	_, evmAddr2 := keeper.MockAddressPair()
	k.SetOrDeleteBalance(db.Ctx(), evmAddr2, 10)
	db.AddBalance(evmAddr2, big.NewInt(-5)) // should redirect to SubBalance
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBigIntTransientModuleState(state.MintedKey), big.NewInt(0))
	require.Equal(t, db.GetBigIntTransientModuleState(state.DeficitKey), big.NewInt(-10))
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(5))
	require.Equal(t, db.GetBalance(evmAddr2), big.NewInt(5))

	// insufficient balance
	db.SubBalance(evmAddr2, big.NewInt(10))
	require.NotNil(t, db.Err())
}

func TestCheckBalance(t *testing.T) {
	k, _, ctx := keeper.MockEVMKeeper()
	db := state.NewStateDBImpl(ctx, k)
	require.Nil(t, db.CheckBalance())

	db.WithErr(errors.New("test"))
	require.NotNil(t, db.CheckBalance())
	db.WithErr(nil)

	seiAddr, evmAddr := keeper.MockAddressPair()
	k.SetAddressMapping(db.Ctx(), seiAddr, evmAddr)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(15)))
	k.BankKeeper().MintCoins(db.Ctx(), types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(db.Ctx(), types.ModuleName, seiAddr, amt)
	db.SubBalance(evmAddr, big.NewInt(10))
	require.Nil(t, db.CheckBalance())

	k, _, ctx = keeper.MockEVMKeeper()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	k.BankKeeper().MintCoins(ctx, types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, seiAddr, amt)
	db = state.NewStateDBImpl(ctx, k)
	db.AddBalance(evmAddr, big.NewInt(1))
	require.NotNil(t, db.CheckBalance()) // deficit imbalance because EVM module hasn't received the credit from SubBalance yet
	db.SubBalance(evmAddr, big.NewInt(1))
	require.Nil(t, db.CheckBalance())
}

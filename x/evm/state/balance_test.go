package state_test

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/core/tracing"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestAddBalance(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	db := state.NewDBImpl(ctx, k, false)
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	db.AddBalance(evmAddr, big.NewInt(0), tracing.BalanceChangeUnspecified)

	// set helpers
	k.SetAddressMapping(db.Ctx(), seiAddr, evmAddr)
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	db.AddBalance(evmAddr, big.NewInt(10000000000000), tracing.BalanceChangeUnspecified)
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(10000000000000))

	_, evmAddr2 := testkeeper.MockAddressPair()
	db.SubBalance(evmAddr2, big.NewInt(-5000000000000), tracing.BalanceChangeUnspecified) // should redirect to AddBalance
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(10000000000000))
	require.Equal(t, db.GetBalance(evmAddr2), big.NewInt(5000000000000))

	_, evmAddr3 := testkeeper.MockAddressPair()
	db.SelfDestruct(evmAddr3)
	db.AddBalance(evmAddr2, big.NewInt(5000000000000), tracing.BalanceChangeUnspecified)
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBalance(evmAddr3), big.NewInt(0))
}

func TestSubBalance(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	db := state.NewDBImpl(ctx, k, false)
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	db.SubBalance(evmAddr, big.NewInt(0), tracing.BalanceChangeUnspecified)

	// set helpers
	k.SetAddressMapping(db.Ctx(), seiAddr, evmAddr)
	require.Equal(t, big.NewInt(0), db.GetBalance(evmAddr))
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(20)))
	k.BankKeeper().MintCoins(db.Ctx(), types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(db.Ctx(), types.ModuleName, seiAddr, amt)
	db.SubBalance(evmAddr, big.NewInt(10000000000000), tracing.BalanceChangeUnspecified)
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(10000000000000))

	_, evmAddr2 := testkeeper.MockAddressPair()
	amt = sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(10)))
	k.BankKeeper().MintCoins(db.Ctx(), types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(db.Ctx(), types.ModuleName, sdk.AccAddress(evmAddr2[:]), amt)
	db.AddBalance(evmAddr2, big.NewInt(-5000000000000), tracing.BalanceChangeUnspecified) // should redirect to SubBalance
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBalance(evmAddr), big.NewInt(10000000000000))
	require.Equal(t, db.GetBalance(evmAddr2), big.NewInt(5000000000000))

	// insufficient balance
	db.SubBalance(evmAddr2, big.NewInt(10000000000000), tracing.BalanceChangeUnspecified)
	require.NotNil(t, db.Err())

	db.WithErr(nil)
	_, evmAddr3 := testkeeper.MockAddressPair()
	db.SelfDestruct(evmAddr3)
	db.SubBalance(evmAddr2, big.NewInt(5000000000000), tracing.BalanceChangeUnspecified)
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBalance(evmAddr3), big.NewInt(0))
}

func TestSetBalance(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	db := state.NewDBImpl(ctx, k, true)
	_, evmAddr := testkeeper.MockAddressPair()
	db.SetBalance(evmAddr, big.NewInt(10000000000000), tracing.BalanceChangeUnspecified)
	require.Equal(t, big.NewInt(10000000000000), db.GetBalance(evmAddr))

	seiAddr2, evmAddr2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(db.Ctx(), seiAddr2, evmAddr2)
	db.SetBalance(evmAddr2, big.NewInt(10000000000000), tracing.BalanceChangeUnspecified)
	require.Equal(t, big.NewInt(10000000000000), db.GetBalance(evmAddr2))
}

func TestSurplus(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, evmAddr := testkeeper.MockAddressPair()

	// test negative usei surplus negative wei surplus
	db := state.NewDBImpl(ctx, k, false)
	db.AddBalance(evmAddr, big.NewInt(1_000_000_000_001), tracing.BalanceChangeUnspecified)
	_, err := db.Finalize()
	require.Nil(t, err)

	// test negative usei surplus positive wei surplus (negative total)
	db = state.NewDBImpl(ctx, k, false)
	db.AddBalance(evmAddr, big.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
	db.SubBalance(evmAddr, big.NewInt(1), tracing.BalanceChangeUnspecified)
	_, err = db.Finalize()
	require.Nil(t, err)

	// test negative usei surplus positive wei surplus (positive total)
	db = state.NewDBImpl(ctx, k, false)
	db.AddBalance(evmAddr, big.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
	db.SubBalance(evmAddr, big.NewInt(2), tracing.BalanceChangeUnspecified)
	db.SubBalance(evmAddr, big.NewInt(999_999_999_999), tracing.BalanceChangeUnspecified)
	surplus, err := db.Finalize()
	require.Nil(t, err)
	require.Equal(t, sdk.OneInt(), surplus)

	// test positive usei surplus negative wei surplus (negative total)
	db = state.NewDBImpl(ctx, k, false)
	db.SubBalance(evmAddr, big.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
	db.AddBalance(evmAddr, big.NewInt(2), tracing.BalanceChangeUnspecified)
	db.AddBalance(evmAddr, big.NewInt(999_999_999_999), tracing.BalanceChangeUnspecified)
	_, err = db.Finalize()
	require.Nil(t, err)

	// test positive usei surplus negative wei surplus (positive total)
	db = state.NewDBImpl(ctx, k, false)
	db.SubBalance(evmAddr, big.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
	db.AddBalance(evmAddr, big.NewInt(999_999_999_999), tracing.BalanceChangeUnspecified)
	surplus, err = db.Finalize()
	require.Nil(t, err)
	require.Equal(t, sdk.OneInt(), surplus)

	// test snapshots
	db = state.NewDBImpl(ctx, k, false)
	db.SubBalance(evmAddr, big.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
	db.AddBalance(evmAddr, big.NewInt(999_999_999_999), tracing.BalanceChangeUnspecified)
	db.Snapshot()
	db.SubBalance(evmAddr, big.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
	db.AddBalance(evmAddr, big.NewInt(999_999_999_999), tracing.BalanceChangeUnspecified)
	db.Snapshot()
	db.SubBalance(evmAddr, big.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
	db.AddBalance(evmAddr, big.NewInt(999_999_999_999), tracing.BalanceChangeUnspecified)
	surplus, err = db.Finalize()
	require.Nil(t, err)
	require.Equal(t, sdk.NewInt(3), surplus)
}

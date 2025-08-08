package state_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestAddBalance(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	db := state.NewDBImpl(ctx, k, false)
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	require.Equal(t, uint256.NewInt(0), db.GetBalance(evmAddr))
	db.AddBalance(evmAddr, uint256.NewInt(0), tracing.BalanceChangeUnspecified)

	// set association
	k.SetAddressMapping(db.Ctx(), seiAddr, evmAddr)
	require.Equal(t, uint256.NewInt(0), db.GetBalance(evmAddr))
	db.AddBalance(evmAddr, uint256.NewInt(10000000000000), tracing.BalanceChangeUnspecified)
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBalance(evmAddr), uint256.NewInt(10000000000000))
}

func TestSubBalance(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	db := state.NewDBImpl(ctx, k, false)
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	require.Equal(t, uint256.NewInt(0), db.GetBalance(evmAddr))
	db.SubBalance(evmAddr, uint256.NewInt(0), tracing.BalanceChangeUnspecified)

	// set association
	k.SetAddressMapping(db.Ctx(), seiAddr, evmAddr)
	require.Equal(t, uint256.NewInt(0), db.GetBalance(evmAddr))
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(20)))
	k.BankKeeper().MintCoins(db.Ctx(), types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(db.Ctx(), types.ModuleName, seiAddr, amt)
	db.SubBalance(evmAddr, uint256.NewInt(10000000000000), tracing.BalanceChangeUnspecified)
	require.Nil(t, db.Err())
	require.Equal(t, db.GetBalance(evmAddr), uint256.NewInt(10000000000000))
}

func TestSetBalance(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	db := state.NewDBImpl(ctx, k, true)
	_, evmAddr := testkeeper.MockAddressPair()
	db.SetBalance(evmAddr, uint256.NewInt(10000000000000), tracing.BalanceChangeUnspecified)
	require.Equal(t, uint256.NewInt(10000000000000), db.GetBalance(evmAddr))

	seiAddr2, evmAddr2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(db.Ctx(), seiAddr2, evmAddr2)
	db.SetBalance(evmAddr2, uint256.NewInt(10000000000000), tracing.BalanceChangeUnspecified)
	require.Equal(t, uint256.NewInt(10000000000000), db.GetBalance(evmAddr2))
}

func TestSurplus(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, evmAddr := testkeeper.MockAddressPair()

	// test negative usei surplus negative wei surplus
	db := state.NewDBImpl(ctx, k, false)
	db.AddBalance(evmAddr, uint256.NewInt(1_000_000_000_001), tracing.BalanceChangeUnspecified)
	_, err := db.Finalize()
	require.Nil(t, err)

	// test negative usei surplus positive wei surplus (negative total)
	db = state.NewDBImpl(ctx, k, false)
	db.AddBalance(evmAddr, uint256.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
	db.SubBalance(evmAddr, uint256.NewInt(1), tracing.BalanceChangeUnspecified)
	_, err = db.Finalize()
	require.Nil(t, err)

	// test negative usei surplus positive wei surplus (positive total)
	db = state.NewDBImpl(ctx, k, false)
	db.AddBalance(evmAddr, uint256.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
	db.SubBalance(evmAddr, uint256.NewInt(2), tracing.BalanceChangeUnspecified)
	db.SubBalance(evmAddr, uint256.NewInt(999_999_999_999), tracing.BalanceChangeUnspecified)
	surplus, err := db.Finalize()
	require.Nil(t, err)
	require.Equal(t, sdk.OneInt(), surplus)

	// test positive usei surplus negative wei surplus (negative total)
	db = state.NewDBImpl(ctx, k, false)
	db.SubBalance(evmAddr, uint256.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
	db.AddBalance(evmAddr, uint256.NewInt(2), tracing.BalanceChangeUnspecified)
	db.AddBalance(evmAddr, uint256.NewInt(999_999_999_999), tracing.BalanceChangeUnspecified)
	_, err = db.Finalize()
	require.Nil(t, err)

	// test positive usei surplus negative wei surplus (positive total)
	db = state.NewDBImpl(ctx, k, false)
	db.SubBalance(evmAddr, uint256.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
	db.AddBalance(evmAddr, uint256.NewInt(999_999_999_999), tracing.BalanceChangeUnspecified)
	surplus, err = db.Finalize()
	require.Nil(t, err)
	require.Equal(t, sdk.OneInt(), surplus)

	// test snapshots
	db = state.NewDBImpl(ctx, k, false)
	db.SubBalance(evmAddr, uint256.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
	db.AddBalance(evmAddr, uint256.NewInt(999_999_999_999), tracing.BalanceChangeUnspecified)
	db.Snapshot()
	db.SubBalance(evmAddr, uint256.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
	db.AddBalance(evmAddr, uint256.NewInt(999_999_999_999), tracing.BalanceChangeUnspecified)
	db.Snapshot()
	db.SubBalance(evmAddr, uint256.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
	db.AddBalance(evmAddr, uint256.NewInt(999_999_999_999), tracing.BalanceChangeUnspecified)
	surplus, err = db.Finalize()
	require.Nil(t, err)
	require.Equal(t, sdk.NewInt(3), surplus)
}

package state_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	vestingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestGetCodeHashCachesBalanceForNoCodeAddress(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, evmAddr := testkeeper.MockAddressPair()
	db := state.NewDBImpl(ctx, k, false)
	meter := db.Ctx().GasMeter()

	before := meter.GasConsumed()
	require.Equal(t, common.Hash{}, db.GetCodeHash(evmAddr))
	firstReadCost := meter.GasConsumed() - before

	before = meter.GasConsumed()
	require.Equal(t, common.Hash{}, db.GetCodeHash(evmAddr))
	secondReadCost := meter.GasConsumed() - before

	require.Less(t, secondReadCost, firstReadCost)
}

func TestBalanceCacheTracksWritesAndReverts(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, evmAddr := testkeeper.MockAddressPair()
	db := state.NewDBImpl(ctx, k, false)

	require.Equal(t, common.Hash{}, db.GetCodeHash(evmAddr))
	db.AddBalance(evmAddr, uint256.NewInt(1), tracing.BalanceChangeUnspecified)
	require.Equal(t, ethtypes.EmptyCodeHash, db.GetCodeHash(evmAddr))

	revision := db.Snapshot()
	db.SubBalance(evmAddr, uint256.NewInt(1), tracing.BalanceChangeUnspecified)
	require.Equal(t, common.Hash{}, db.GetCodeHash(evmAddr))

	db.RevertToSnapshot(revision)
	require.Equal(t, ethtypes.EmptyCodeHash, db.GetCodeHash(evmAddr))
}

func TestBalanceCacheInvalidatedByAccountWrite(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	now := time.Now()
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(now)
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	db := state.NewDBImpl(ctx, k, false)
	k.SetAddressMapping(db.Ctx(), seiAddr, evmAddr)

	coins := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.OneInt()))
	baseAccount := authtypes.NewBaseAccountWithAddress(seiAddr)
	vestingAccount := vestingtypes.NewContinuousVestingAccount(baseAccount, coins, now.Unix(), now.Add(time.Hour).Unix(), nil)
	k.AccountKeeper().SetAccount(db.Ctx(), vestingAccount)
	require.NoError(t, k.BankKeeper().MintCoins(db.Ctx(), types.ModuleName, coins))
	require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(db.Ctx(), types.ModuleName, seiAddr, coins))
	require.Zero(t, db.GetBalance(evmAddr).Sign())

	k.AccountKeeper().SetAccount(db.Ctx(), baseAccount)
	require.Equal(t, uint256.NewInt(1_000_000_000_000), db.GetBalance(evmAddr))
}

func TestBalanceCacheSharedWithNestedStateDB(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, evmAddr := testkeeper.MockAddressPair()
	outer := state.NewDBImpl(ctx, k, false)
	require.Zero(t, outer.GetBalance(evmAddr).Sign())

	inner := state.NewDBImpl(outer.Ctx(), k, false)
	inner.AddBalance(evmAddr, uint256.NewInt(1), tracing.BalanceChangeUnspecified)
	_, err := inner.Finalize()
	require.NoError(t, err)

	require.Equal(t, uint256.NewInt(1), outer.GetBalance(evmAddr))
}

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

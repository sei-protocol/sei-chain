package state_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	testkeeper "github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/giga/deps/xevm/keeper"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
	evmtypes "github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
)

type recipientCheckerRegistrar interface {
	RegisterRecipientChecker(func(sdk.Context, sdk.AccAddress) bool)
}

func newMappedStateDB(t *testing.T) (*state.DBImpl, *evmkeeper.Keeper, sdk.AccAddress, common.Address) {
	t.Helper()
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	db := state.NewDBImpl(ctx, k, false)
	k.SetAddressMapping(db.Ctx(), seiAddr, evmAddr)
	return db, k, seiAddr, evmAddr
}

func fundUsei(t *testing.T, k *evmkeeper.Keeper, ctx sdk.Context, seiAddr sdk.AccAddress, amount int64) {
	t.Helper()
	coins := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(amount)))
	require.NoError(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, coins))
	require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, coins))
}

func TestSetNonceCallsV2LoggerWithPreviousNonce(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	db := state.NewDBImpl(ctx, k, false)
	_, evmAddr := testkeeper.MockAddressPair()

	db.SetNonce(evmAddr, 7, tracing.NonceChangeUnspecified)

	var gotAddr common.Address
	var gotPrev, gotNew uint64
	var gotReason tracing.NonceChangeReason
	db.SetLogger(&tracing.Hooks{
		OnNonceChangeV2: func(addr common.Address, prev, new uint64, reason tracing.NonceChangeReason) {
			gotAddr = addr
			gotPrev = prev
			gotNew = new
			gotReason = reason
		},
	})

	db.SetNonce(evmAddr, 9, tracing.NonceChangeEoACall)

	require.Equal(t, evmAddr, gotAddr)
	require.EqualValues(t, 7, gotPrev)
	require.EqualValues(t, 9, gotNew)
	require.Equal(t, tracing.NonceChangeEoACall, gotReason)
}

func TestCleanupClearsSnapshotBookkeeping(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	db := state.NewDBImpl(ctx, k, false)
	db.Snapshot()

	require.NotPanics(t, db.Cleanup)
}

func TestCopyDeepCopiesJournalSnapshotState(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	db := state.NewDBImpl(ctx, k, false)
	_, evmAddr := testkeeper.MockAddressPair()
	slot := common.BytesToHash([]byte("slot"))
	transientValue := common.BytesToHash([]byte("transient"))
	copyErr := errors.New("copy me")
	precompileErr := errors.New("precompile")

	db.Ctx().EventManager().EmitEvent(sdk.NewEvent("before-copy"))
	db.Snapshot()
	db.SetTransientState(evmAddr, slot, transientValue)
	db.MarkAccount(evmAddr, state.AccountCreated)
	db.AddRefund(5)
	db.AddSlotToAccessList(evmAddr, slot)
	db.AddLog(&ethtypes.Log{Address: evmAddr})
	db.AddBalance(evmAddr, uint256.NewInt(7), tracing.BalanceChangeUnspecified)
	db.WithErr(copyErr)
	db.SetPrecompileError(precompileErr)

	copied, ok := db.Copy().(*state.DBImpl)
	require.True(t, ok)
	require.Equal(t, copyErr, copied.Err())
	require.Equal(t, precompileErr, copied.GetPrecompileError())
	require.Equal(t, transientValue, copied.GetTransientState(evmAddr, slot))
	require.True(t, copied.Created(evmAddr))
	require.True(t, copied.AddressInAccessList(evmAddr))
	_, slotOK := copied.SlotInAccessList(evmAddr, slot)
	require.True(t, slotOK)
	require.EqualValues(t, 5, copied.GetRefund())
	require.Len(t, copied.GetAllLogs(), 1)
	require.Equal(t, uint256.NewInt(7), copied.GetBalance(evmAddr))

	db.SetTransientState(evmAddr, slot, common.Hash{1})
	db.MarkAccount(evmAddr, state.AccountDeleted)
	db.AddRefund(3)
	db.AddSlotToAccessList(evmAddr, common.Hash{2})
	db.AddLog(&ethtypes.Log{Address: common.Address{3}})
	db.AddBalance(evmAddr, uint256.NewInt(1), tracing.BalanceChangeUnspecified)

	require.Equal(t, transientValue, copied.GetTransientState(evmAddr, slot))
	require.True(t, copied.Created(evmAddr))
	require.EqualValues(t, 5, copied.GetRefund())
	require.Len(t, copied.GetAllLogs(), 1)
	_, ok = copied.SlotInAccessList(evmAddr, common.Hash{2})
	require.False(t, ok)
}

func TestCreateAccountSkipsMalformedRawStorageKey(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	db := state.NewDBImpl(ctx, k, false)
	_, evmAddr := testkeeper.MockAddressPair()

	db.SetCode(evmAddr, []byte("code"))
	k.GetKVStore(db.Ctx()).Set(evmtypes.StateKey(evmAddr), []byte{1})

	require.NotPanics(t, func() { db.CreateAccount(evmAddr) })
}

func TestSnapshotRevertBalanceUsesCacheJournal(t *testing.T) {
	t.Run("sub unlocked coins", func(t *testing.T) {
		db, k, seiAddr, evmAddr := newMappedStateDB(t)
		coins := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(db.Ctx()), sdk.NewInt(1)))
		balanceBefore := db.GetBalance(evmAddr)

		rev := db.Snapshot()
		db.AddBalance(evmAddr, uint256.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
		require.NoError(t, db.Err())
		require.NoError(t, k.BankKeeper().SubUnlockedCoins(db.Ctx(), seiAddr, coins, true))

		require.NotPanics(t, func() { db.RevertToSnapshot(rev) })
		require.Equal(t, balanceBefore.ToBig(), db.GetBalance(evmAddr).ToBig())
	})

	t.Run("sub wei", func(t *testing.T) {
		db, k, seiAddr, evmAddr := newMappedStateDB(t)
		balanceBefore := db.GetBalance(evmAddr)

		rev := db.Snapshot()
		db.AddBalance(evmAddr, uint256.NewInt(1), tracing.BalanceChangeUnspecified)
		require.NoError(t, db.Err())
		require.NoError(t, k.BankKeeper().SubWei(db.Ctx(), seiAddr, sdk.NewInt(1)))

		require.NotPanics(t, func() { db.RevertToSnapshot(rev) })
		require.Equal(t, balanceBefore.ToBig(), db.GetBalance(evmAddr).ToBig())
	})

	t.Run("add coins", func(t *testing.T) {
		db, k, seiAddr, evmAddr := newMappedStateDB(t)
		fundUsei(t, k, db.Ctx(), seiAddr, 1)
		balanceBefore := db.GetBalance(evmAddr)

		rev := db.Snapshot()
		db.SubBalance(evmAddr, uint256.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
		require.NoError(t, db.Err())
		registrar := k.BankKeeper().(recipientCheckerRegistrar)
		registrar.RegisterRecipientChecker(func(sdk.Context, sdk.AccAddress) bool { return false })

		require.NotPanics(t, func() { db.RevertToSnapshot(rev) })
		require.Equal(t, balanceBefore.ToBig(), db.GetBalance(evmAddr).ToBig())
	})

	t.Run("add wei", func(t *testing.T) {
		db, k, seiAddr, evmAddr := newMappedStateDB(t)
		require.NoError(t, k.BankKeeper().AddWei(db.Ctx(), seiAddr, sdk.NewInt(1)))
		balanceBefore := db.GetBalance(evmAddr)

		rev := db.Snapshot()
		db.SubBalance(evmAddr, uint256.NewInt(1), tracing.BalanceChangeUnspecified)
		require.NoError(t, db.Err())
		registrar := k.BankKeeper().(recipientCheckerRegistrar)
		calls := 0
		registrar.RegisterRecipientChecker(func(sdk.Context, sdk.AccAddress) bool {
			calls++
			return calls == 1
		})

		require.NotPanics(t, func() { db.RevertToSnapshot(rev) })
		require.Equal(t, balanceBefore.ToBig(), db.GetBalance(evmAddr).ToBig())
	})
}

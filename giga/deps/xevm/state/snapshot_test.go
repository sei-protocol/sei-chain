package state_test

import (
	"math/big"
	"testing"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	testkeeper "github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	"github.com/stretchr/testify/require"
)

// ----------------------------------------------------------------------------
// KV-state revert: SetState / storageChange
// ----------------------------------------------------------------------------

func TestSnapshotReverts_Storage(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, evmAddr := testkeeper.MockAddressPair()
	key := common.BytesToHash([]byte("slot"))
	val1 := common.BytesToHash([]byte("v1"))
	val2 := common.BytesToHash([]byte("v2"))

	sdb.SetState(evmAddr, key, val1)
	require.Equal(t, val1, sdb.GetState(evmAddr, key))

	rev := sdb.Snapshot()
	sdb.SetState(evmAddr, key, val2)
	require.Equal(t, val2, sdb.GetState(evmAddr, key))

	sdb.RevertToSnapshot(rev)
	require.Equal(t, val1, sdb.GetState(evmAddr, key))
}

func TestSnapshotReverts_StorageToZero(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, evmAddr := testkeeper.MockAddressPair()
	key := common.BytesToHash([]byte("slot"))
	val := common.BytesToHash([]byte("v1"))

	sdb.SetState(evmAddr, key, val)
	rev := sdb.Snapshot()

	// Overwrite with zero (deletes the KV entry)
	sdb.SetState(evmAddr, key, common.Hash{})
	require.Equal(t, common.Hash{}, sdb.GetState(evmAddr, key))

	sdb.RevertToSnapshot(rev)
	// Slot should be restored to non-zero
	require.Equal(t, val, sdb.GetState(evmAddr, key))
}

func TestSnapshotReverts_MultipleStorageSlots(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, evmAddr := testkeeper.MockAddressPair()
	keyA := common.BytesToHash([]byte("A"))
	keyB := common.BytesToHash([]byte("B"))
	valA := common.BytesToHash([]byte("valA"))
	valB := common.BytesToHash([]byte("valB"))

	sdb.SetState(evmAddr, keyA, valA)

	rev := sdb.Snapshot()
	sdb.SetState(evmAddr, keyA, common.BytesToHash([]byte("new")))
	sdb.SetState(evmAddr, keyB, valB)

	sdb.RevertToSnapshot(rev)
	require.Equal(t, valA, sdb.GetState(evmAddr, keyA))
	require.Equal(t, common.Hash{}, sdb.GetState(evmAddr, keyB))
}

// ----------------------------------------------------------------------------
// KV-state revert: SetCode / codeChange
// ----------------------------------------------------------------------------

func TestSnapshotReverts_Code(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, evmAddr := testkeeper.MockAddressPair()

	sdb.SetCode(evmAddr, []byte("v1"))
	require.Equal(t, []byte("v1"), sdb.GetCode(evmAddr))

	rev := sdb.Snapshot()
	sdb.SetCode(evmAddr, []byte("v2"))
	require.Equal(t, []byte("v2"), sdb.GetCode(evmAddr))

	sdb.RevertToSnapshot(rev)
	require.Equal(t, []byte("v1"), sdb.GetCode(evmAddr))
}

func TestSnapshotReverts_CodeToEmpty(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, evmAddr := testkeeper.MockAddressPair()

	// No code initially; snapshot; set code; revert → no code again.
	rev := sdb.Snapshot()
	sdb.SetCode(evmAddr, []byte("deployed"))
	require.Equal(t, []byte("deployed"), sdb.GetCode(evmAddr))
	require.NotZero(t, sdb.GetCodeSize(evmAddr))

	sdb.RevertToSnapshot(rev)
	require.Nil(t, sdb.GetCode(evmAddr))
	require.Zero(t, sdb.GetCodeSize(evmAddr))
}

// ----------------------------------------------------------------------------
// KV-state revert: SetNonce / nonceChange
// ----------------------------------------------------------------------------

func TestSnapshotReverts_Nonce(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, evmAddr := testkeeper.MockAddressPair()

	sdb.SetNonce(evmAddr, 5, tracing.NonceChangeUnspecified)
	rev := sdb.Snapshot()

	sdb.SetNonce(evmAddr, 10, tracing.NonceChangeUnspecified)
	require.EqualValues(t, 10, sdb.GetNonce(evmAddr))

	sdb.RevertToSnapshot(rev)
	require.EqualValues(t, 5, sdb.GetNonce(evmAddr))
}

// ----------------------------------------------------------------------------
// Balance revert: AddBalance / SubBalance / balanceChange
// ----------------------------------------------------------------------------

func TestSnapshotReverts_AddBalance(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)

	// Fund the account first (outside stateDB so it's in committed state)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000)))
	require.NoError(t, k.BankKeeper().MintCoins(sdb.Ctx(), types.ModuleName, amt))
	require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(sdb.Ctx(), types.ModuleName, seiAddr, amt))

	balanceBefore := sdb.GetBalance(evmAddr)
	rev := sdb.Snapshot()

	// Add 1 usei worth of wei (1e12 wei)
	oneUsei := uint256.NewInt(1_000_000_000_000)
	sdb.AddBalance(evmAddr, oneUsei, tracing.BalanceChangeUnspecified)
	require.Nil(t, sdb.Err())
	require.Equal(t, new(big.Int).Add(balanceBefore.ToBig(), oneUsei.ToBig()), sdb.GetBalance(evmAddr).ToBig())

	sdb.RevertToSnapshot(rev)
	require.Equal(t, balanceBefore.ToBig(), sdb.GetBalance(evmAddr).ToBig())
}

func TestSnapshotReverts_SubBalance(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)

	// Fund with 10 usei
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(10)))
	require.NoError(t, k.BankKeeper().MintCoins(sdb.Ctx(), types.ModuleName, amt))
	require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(sdb.Ctx(), types.ModuleName, seiAddr, amt))

	balanceBefore := sdb.GetBalance(evmAddr)
	rev := sdb.Snapshot()

	oneUsei := uint256.NewInt(1_000_000_000_000)
	sdb.SubBalance(evmAddr, oneUsei, tracing.BalanceChangeUnspecified)
	require.Nil(t, sdb.Err())
	require.Equal(t, new(big.Int).Sub(balanceBefore.ToBig(), oneUsei.ToBig()), sdb.GetBalance(evmAddr).ToBig())

	sdb.RevertToSnapshot(rev)
	require.Equal(t, balanceBefore.ToBig(), sdb.GetBalance(evmAddr).ToBig())
}

// ----------------------------------------------------------------------------
// Account creation: createAccountChange (clearAccountStateJournaled)
// ----------------------------------------------------------------------------

func TestSnapshotReverts_CreateAccount_ClearsAndRestores(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, evmAddr := testkeeper.MockAddressPair()
	key := common.BytesToHash([]byte("slot"))
	val := common.BytesToHash([]byte("v"))

	// Set up code + storage so the account has state to clear.
	sdb.SetCode(evmAddr, []byte("existing_code"))
	sdb.SetState(evmAddr, key, val)
	sdb.SetNonce(evmAddr, 3, tracing.NonceChangeUnspecified)

	rev := sdb.Snapshot()

	// CreateAccount clears the existing state.
	sdb.CreateAccount(evmAddr)
	require.Nil(t, sdb.GetCode(evmAddr))
	require.Equal(t, common.Hash{}, sdb.GetState(evmAddr, key))
	require.EqualValues(t, 0, sdb.GetNonce(evmAddr))

	// After revert, old code/storage/nonce must come back.
	sdb.RevertToSnapshot(rev)
	require.Equal(t, []byte("existing_code"), sdb.GetCode(evmAddr))
	require.Equal(t, val, sdb.GetState(evmAddr, key))
	require.EqualValues(t, 3, sdb.GetNonce(evmAddr))
}

func TestSnapshotReverts_CreateAccountOnFreshAddress_NoOp(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, evmAddr := testkeeper.MockAddressPair()

	// Address has no prior state; CreateAccount should not panic.
	rev := sdb.Snapshot()
	sdb.CreateAccount(evmAddr)
	sdb.RevertToSnapshot(rev)
	// No assertion needed; just verify no panic and code is still absent.
	require.Nil(t, sdb.GetCode(evmAddr))
}

// ----------------------------------------------------------------------------
// SelfDestruct: deleteMappingChange
// ----------------------------------------------------------------------------

func TestSnapshotReverts_SelfDestruct_RestoreMapping(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)

	// Confirm mapping exists.
	got, ok := k.GetSeiAddress(sdb.Ctx(), evmAddr)
	require.True(t, ok)
	require.Equal(t, seiAddr, got)

	sdb.CreateAccount(evmAddr)
	sdb.MarkAccount(evmAddr, state.AccountCreated)

	rev := sdb.Snapshot()
	sdb.SelfDestruct(evmAddr)

	// After SelfDestruct the mapping must be gone.
	_, ok = k.GetSeiAddress(sdb.Ctx(), evmAddr)
	require.False(t, ok)

	sdb.RevertToSnapshot(rev)

	// After revert the mapping must be restored.
	got2, ok2 := k.GetSeiAddress(sdb.Ctx(), evmAddr)
	require.True(t, ok2)
	require.Equal(t, seiAddr, got2)
}

// ----------------------------------------------------------------------------
// GetCommittedState: reads pre-stateDB state (bypasses stateDB's CMS)
// ----------------------------------------------------------------------------

func TestGetCommittedState_PreservesOriginal(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, evmAddr := testkeeper.MockAddressPair()
	key := common.BytesToHash([]byte("slot"))

	// SetState writes to the stateDB's CMS; GetCommittedState reads below it.
	sdb.SetState(evmAddr, key, common.BytesToHash([]byte("v1")))
	require.Equal(t, common.Hash{}, sdb.GetCommittedState(evmAddr, key))

	rev := sdb.Snapshot()
	sdb.SetState(evmAddr, key, common.BytesToHash([]byte("v2")))
	require.Equal(t, common.Hash{}, sdb.GetCommittedState(evmAddr, key))

	sdb.RevertToSnapshot(rev)
	require.Equal(t, common.Hash{}, sdb.GetCommittedState(evmAddr, key))
}

// ----------------------------------------------------------------------------
// Nested snapshots
// ----------------------------------------------------------------------------

func TestNestedSnapshots_InnerRevertOuterCommit(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, evmAddr := testkeeper.MockAddressPair()
	key := common.BytesToHash([]byte("k"))
	v1 := common.BytesToHash([]byte("v1"))
	v2 := common.BytesToHash([]byte("v2"))
	v3 := common.BytesToHash([]byte("v3"))

	sdb.SetState(evmAddr, key, v1)
	outer := sdb.Snapshot()

	sdb.SetState(evmAddr, key, v2)
	inner := sdb.Snapshot()

	sdb.SetState(evmAddr, key, v3)

	// Revert inner only.
	sdb.RevertToSnapshot(inner)
	require.Equal(t, v2, sdb.GetState(evmAddr, key))

	// Revert outer.
	sdb.RevertToSnapshot(outer)
	require.Equal(t, v1, sdb.GetState(evmAddr, key))
}

func TestNestedSnapshots_BothReverted(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, evmAddr := testkeeper.MockAddressPair()
	key := common.BytesToHash([]byte("k"))

	outer := sdb.Snapshot()

	sdb.SetState(evmAddr, key, common.BytesToHash([]byte("a")))
	inner := sdb.Snapshot()
	sdb.SetState(evmAddr, key, common.BytesToHash([]byte("b")))

	// Revert inner, then outer.
	sdb.RevertToSnapshot(inner)
	sdb.RevertToSnapshot(outer)
	require.Equal(t, common.Hash{}, sdb.GetState(evmAddr, key))
}

func TestNestedSnapshots_MultipleTypes(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, evmAddr := testkeeper.MockAddressPair()
	key := common.BytesToHash([]byte("slot"))
	tkey := common.BytesToHash([]byte("tslot"))

	sdb.SetState(evmAddr, key, common.BytesToHash([]byte("s1")))
	sdb.SetTransientState(evmAddr, tkey, common.BytesToHash([]byte("t1")))
	sdb.SetNonce(evmAddr, 1, tracing.NonceChangeUnspecified)

	rev := sdb.Snapshot()

	sdb.SetState(evmAddr, key, common.BytesToHash([]byte("s2")))
	sdb.SetTransientState(evmAddr, tkey, common.BytesToHash([]byte("t2")))
	sdb.SetNonce(evmAddr, 2, tracing.NonceChangeUnspecified)
	sdb.SetCode(evmAddr, []byte("code"))

	sdb.RevertToSnapshot(rev)

	require.Equal(t, common.BytesToHash([]byte("s1")), sdb.GetState(evmAddr, key))
	require.Equal(t, common.BytesToHash([]byte("t1")), sdb.GetTransientState(evmAddr, tkey))
	require.EqualValues(t, 1, sdb.GetNonce(evmAddr))
	require.Nil(t, sdb.GetCode(evmAddr))
}

// ----------------------------------------------------------------------------
// Event manager isolation: reverted events must not surface at Finalize
// ----------------------------------------------------------------------------

func TestSnapshotReverts_EventsDiscarded(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	before := len(ctx.EventManager().Events())

	// Emit one event before snapshot.
	sdb.Ctx().EventManager().EmitEvent(sdk.NewEvent("pre"))

	rev := sdb.Snapshot()
	// Emit one event inside snapshot.
	sdb.Ctx().EventManager().EmitEvent(sdk.NewEvent("inside"))

	sdb.RevertToSnapshot(rev)

	_, err := sdb.Finalize()
	require.NoError(t, err)

	// Only the pre-snapshot event should reach the outer ctx.
	after := len(ctx.EventManager().Events())
	require.Equal(t, before+1, after)
}

func TestSnapshotReverts_EventsPreservedAcrossNestedReverts(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	before := len(ctx.EventManager().Events())

	sdb.Ctx().EventManager().EmitEvent(sdk.NewEvent("e0"))

	outer := sdb.Snapshot()
	sdb.Ctx().EventManager().EmitEvent(sdk.NewEvent("e1"))

	inner := sdb.Snapshot()
	sdb.Ctx().EventManager().EmitEvent(sdk.NewEvent("e2"))

	sdb.RevertToSnapshot(inner)
	sdb.Ctx().EventManager().EmitEvent(sdk.NewEvent("e3"))

	sdb.RevertToSnapshot(outer)
	sdb.Ctx().EventManager().EmitEvent(sdk.NewEvent("e4"))

	_, err := sdb.Finalize()
	require.NoError(t, err)

	// e0 (pre-outer), e4 (post-revert of outer) survive; e1, e2, e3 reverted.
	after := len(ctx.EventManager().Events())
	require.Equal(t, before+2, after)
}

// ----------------------------------------------------------------------------
// Snapshot IDs are monotonically increasing
// ----------------------------------------------------------------------------

func TestSnapshot_IDsMonotonicallyIncrease(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	id0 := sdb.Snapshot()
	id1 := sdb.Snapshot()
	id2 := sdb.Snapshot()

	require.Less(t, id0, id1)
	require.Less(t, id1, id2)
}

func TestSnapshot_InvalidRevisionPanics(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	require.Panics(t, func() { sdb.RevertToSnapshot(999) })
}

// ----------------------------------------------------------------------------
// Isolation: uncommitted stateDB writes not visible to a second stateDB
// ----------------------------------------------------------------------------

func TestSnapshot_WritesNotVisibleBeforeFinalize(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, evmAddr := testkeeper.MockAddressPair()
	key := common.BytesToHash([]byte("slot"))
	val := common.BytesToHash([]byte("v"))

	sdb.SetState(evmAddr, key, val)

	// A fresh stateDB on the same underlying ctx should not see the write.
	sdb2 := state.NewDBImpl(ctx, k, false)
	require.Equal(t, common.Hash{}, sdb2.GetState(evmAddr, key))

	_, err := sdb.Finalize()
	require.NoError(t, err)

	// After Finalize (CMS flushed), a new stateDB sees the value.
	sdb3 := state.NewDBImpl(ctx, k, false)
	require.Equal(t, val, sdb3.GetState(evmAddr, key))
}

// ----------------------------------------------------------------------------
// Refund revert
// ----------------------------------------------------------------------------

func TestSnapshotReverts_Refund(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	sdb.AddRefund(100)
	require.EqualValues(t, 100, sdb.GetRefund())

	rev := sdb.Snapshot()
	sdb.AddRefund(50)
	require.EqualValues(t, 150, sdb.GetRefund())

	sdb.RevertToSnapshot(rev)
	require.EqualValues(t, 100, sdb.GetRefund())
}

// ----------------------------------------------------------------------------
// Access list revert
// ----------------------------------------------------------------------------

func TestSnapshotReverts_AccessList(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, addr1 := testkeeper.MockAddressPair()
	_, addr2 := testkeeper.MockAddressPair()
	slot := common.BytesToHash([]byte("slot"))

	sdb.AddAddressToAccessList(addr1)
	require.True(t, sdb.AddressInAccessList(addr1))

	rev := sdb.Snapshot()
	sdb.AddAddressToAccessList(addr2)
	sdb.AddSlotToAccessList(addr1, slot)

	sdb.RevertToSnapshot(rev)

	require.True(t, sdb.AddressInAccessList(addr1))
	require.False(t, sdb.AddressInAccessList(addr2))
	addrOk, slotOk := sdb.SlotInAccessList(addr1, slot)
	require.True(t, addrOk)
	require.False(t, slotOk)
}

// ----------------------------------------------------------------------------
// Log revert
// ----------------------------------------------------------------------------

func TestSnapshotReverts_Logs(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, evmAddr := testkeeper.MockAddressPair()

	sdb.AddLog(&ethtypes.Log{Address: evmAddr})
	require.Len(t, sdb.GetAllLogs(), 1)

	rev := sdb.Snapshot()
	sdb.AddLog(&ethtypes.Log{Address: evmAddr})
	require.Len(t, sdb.GetAllLogs(), 2)

	sdb.RevertToSnapshot(rev)
	require.Len(t, sdb.GetAllLogs(), 1)
}

// ----------------------------------------------------------------------------
// Multiple reverts to the same (outer) snapshot, skipping an inner one
// ----------------------------------------------------------------------------

func TestSnapshotReverts_SkipInnerSnapshot(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	sdb := state.NewDBImpl(ctx, k, false)

	_, evmAddr := testkeeper.MockAddressPair()
	key := common.BytesToHash([]byte("k"))
	v1 := common.BytesToHash([]byte("v1"))
	v2 := common.BytesToHash([]byte("v2"))

	sdb.SetState(evmAddr, key, v1)
	outer := sdb.Snapshot()

	sdb.SetState(evmAddr, key, v2)
	_ = sdb.Snapshot() // inner snapshot, not used
	sdb.SetState(evmAddr, key, common.BytesToHash([]byte("v3")))

	// Revert directly to outer, bypassing inner.
	sdb.RevertToSnapshot(outer)
	require.Equal(t, v1, sdb.GetState(evmAddr, key))
}

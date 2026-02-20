package state_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
	testkeeper "github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
)

func TestState(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	_, evmAddr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)
	statedb.CreateAccount(evmAddr)
	require.True(t, statedb.Created(evmAddr))
	require.False(t, statedb.HasSelfDestructed(evmAddr))
	statedb.AddBalance(evmAddr, uint256.NewInt(10), tracing.BalanceChangeUnspecified)
	k.BankKeeper().MintCoins(statedb.Ctx(), types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(10))))
	key := common.BytesToHash([]byte("abc"))
	val := common.BytesToHash([]byte("def"))
	statedb.SetState(evmAddr, key, val)
	statedb.SetCode(evmAddr, []byte("code"))
	require.Equal(t, val, statedb.GetState(evmAddr, key))
	require.Equal(t, common.Hash{}, statedb.GetCommittedState(evmAddr, key))
	// fork the store and overwrite the key
	statedb.Snapshot()
	newVal := common.BytesToHash([]byte("ghi"))
	statedb.SetState(evmAddr, key, newVal)
	require.Equal(t, newVal, statedb.GetState(evmAddr, key))
	require.Equal(t, common.Hash{}, statedb.GetCommittedState(evmAddr, key))
	tkey := common.BytesToHash([]byte("jkl"))
	tval := common.BytesToHash([]byte("mno"))
	statedb.SetTransientState(evmAddr, tkey, tval)
	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	// destruct should clear balance, but keep state. Committed state should also be accessible
	// state would be cleared after finalize
	statedb.SelfDestruct(evmAddr)
	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	require.NotEqual(t, common.Hash{}, statedb.GetState(evmAddr, key))
	require.Equal(t, common.Hash{}, statedb.GetCommittedState(evmAddr, key))
	require.Equal(t, uint256.NewInt(0), statedb.GetBalance(evmAddr))
	require.True(t, statedb.HasSelfDestructed(evmAddr))
	statedb.Finalize()
	require.Equal(t, common.Hash{}, statedb.GetState(evmAddr, key))
	// set storage
	statedb.SetStorage(evmAddr, map[common.Hash]common.Hash{{}: {}})
	require.Equal(t, common.Hash{}, statedb.GetState(evmAddr, common.Hash{}))
}

func TestCreate(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	_, evmAddr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)
	statedb.CreateAccount(evmAddr)
	require.False(t, statedb.HasSelfDestructed(evmAddr))
	key := common.BytesToHash([]byte("abc"))
	val := common.BytesToHash([]byte("def"))
	tkey := common.BytesToHash([]byte("jkl"))
	tval := common.BytesToHash([]byte("mno"))
	statedb.SetCode(evmAddr, []byte("code"))
	statedb.SetState(evmAddr, key, val)
	statedb.SetTransientState(evmAddr, tkey, tval)
	statedb.AddBalance(evmAddr, uint256.NewInt(10000000000000), tracing.BalanceChangeUnspecified)
	// recreate an account should clear its state, but keep its balance and transient state
	statedb.CreateAccount(evmAddr)
	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	require.Equal(t, common.Hash{}, statedb.GetState(evmAddr, key))
	require.Equal(t, uint256.NewInt(10000000000000), statedb.GetBalance(evmAddr))
	require.True(t, statedb.Created(evmAddr))
	require.False(t, statedb.HasSelfDestructed(evmAddr))
	// recreate a destructed (in the same tx) account should clear its selfDestructed flag
	statedb.SelfDestruct(evmAddr)
	require.Nil(t, statedb.Err())
	require.True(t, statedb.HasSelfDestructed(evmAddr))
	require.Equal(t, uint256.NewInt(0), statedb.GetBalance(evmAddr))
	statedb.CreateAccount(evmAddr)
	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	require.Equal(t, common.Hash{}, statedb.GetState(evmAddr, key))
	require.Equal(t, uint256.NewInt(0), statedb.GetBalance(evmAddr)) // cleared during SelfDestruct
	require.True(t, statedb.Created(evmAddr))
	require.False(t, statedb.HasSelfDestructed(evmAddr))
}

func TestSelfDestructAssociated(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	statedb := state.NewDBImpl(ctx, k, false)
	statedb.CreateAccount(evmAddr)
	key := common.BytesToHash([]byte("abc"))
	val := common.BytesToHash([]byte("def"))
	tkey := common.BytesToHash([]byte("jkl"))
	tval := common.BytesToHash([]byte("mno"))
	statedb.SetState(evmAddr, key, val)
	statedb.SetCode(evmAddr, []byte("code"))
	statedb.SetTransientState(evmAddr, tkey, tval)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(10)))
	k.BankKeeper().MintCoins(statedb.Ctx(), types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(statedb.Ctx(), types.ModuleName, seiAddr, amt)

	// Selfdestruct6780 should only act if the account is created in the same block
	statedb.MarkAccount(evmAddr, nil)
	statedb.SelfDestruct6780(evmAddr)
	require.Equal(t, val, statedb.GetState(evmAddr, key))
	statedb.MarkAccount(evmAddr, state.AccountCreated)
	require.False(t, statedb.HasSelfDestructed(evmAddr))

	// Selfdestruct6780 is equivalent to SelfDestruct if account is created in the same block
	statedb.SelfDestruct6780(evmAddr)
	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	require.NotEqual(t, common.Hash{}, statedb.GetState(evmAddr, key))
	// there is a bug in SelfDestruct where account association is deleted before balance is handled.
	// This condition passes not because the balance is 0, but because the account association is deleted.
	require.Equal(t, uint256.NewInt(0), statedb.GetBalance(evmAddr))
	require.True(t, statedb.HasSelfDestructed(evmAddr))
	require.False(t, statedb.Created(evmAddr))
	statedb.AddBalance(evmAddr, uint256.NewInt(1), tracing.BalanceChangeUnspecified)
	require.Equal(t, uint256.NewInt(1), statedb.GetBalance(evmAddr))
	statedb.Finalize()
	require.Equal(t, common.Hash{}, statedb.GetState(evmAddr, key))
	// association should also be removed
	_, ok := k.GetSeiAddress(statedb.Ctx(), evmAddr)
	require.False(t, ok)
	// balance in destructed account should be cleared and transferred to coinbase
	require.Equal(t, uint256.NewInt(0), statedb.GetBalance(evmAddr))
	fc, _ := k.GetFeeCollectorAddress(statedb.Ctx())
	require.Equal(t, uint256.NewInt(1), statedb.GetBalance(fc))
}

// TestEIP6780WithPrefundedAddress verifies that EIP-6780 selfdestruct works correctly
// when a contract is deployed to a prefunded address. This tests the fix for a bug where
// contracts deployed to addresses with existing balance would not be destroyed by
// SelfDestruct6780 because CreateAccount() was not called (since the account already existed).
// The fix ensures CreateContract() marks the account as created for EIP-6780 purposes.
func TestEIP6780WithPrefundedAddress(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)

	statedb := state.NewDBImpl(ctx, k, false)

	// Prefund the address with balance using statedb context
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000)))
	k.BankKeeper().MintCoins(statedb.Ctx(), types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(statedb.Ctx(), types.ModuleName, seiAddr, amt)

	// Verify the account has balance but is not marked as "created" yet
	require.True(t, statedb.GetBalance(evmAddr).CmpBig(big.NewInt(0)) > 0, "address should have balance")
	require.False(t, statedb.Created(evmAddr), "account should not be marked as created before CreateContract")

	// Simulate the EVM's contract creation flow for a prefunded address:
	// In go-ethereum's create(), if Exist() returns true (which it does for prefunded addresses),
	// CreateAccount() is NOT called. Instead, only CreateContract() is called.
	// This is the exact scenario that was broken before the fix.
	require.True(t, statedb.Exist(evmAddr), "prefunded address should exist")

	// Only call CreateContract (not CreateAccount) - this simulates the real EVM behavior
	statedb.CreateContract(evmAddr)

	// After CreateContract, the account should be marked as created for EIP-6780
	require.True(t, statedb.Created(evmAddr), "account should be marked as created after CreateContract")

	// Set some contract state
	statedb.SetCode(evmAddr, []byte("contract code"))
	key := common.BytesToHash([]byte("storage_key"))
	val := common.BytesToHash([]byte("storage_value"))
	statedb.SetState(evmAddr, key, val)

	// Now SelfDestruct6780 should work correctly - the key test is that destructed == true
	_, destructed := statedb.SelfDestruct6780(evmAddr)
	require.True(t, destructed, "SelfDestruct6780 should destruct the contract created in same tx")
	require.True(t, statedb.HasSelfDestructed(evmAddr), "account should be marked as self-destructed")

	// Finalize to clear the state
	statedb.Finalize()

	// After finalize, the contract's state should be cleared
	require.Equal(t, common.Hash{}, statedb.GetState(evmAddr, key), "storage should be cleared after finalize")
}

func TestSnapshot(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	eventCount := len(ctx.EventManager().Events())
	statedb := state.NewDBImpl(ctx, k, false)
	statedb.Snapshot()
	statedb.CreateAccount(evmAddr)
	key := common.BytesToHash([]byte("abc"))
	val := common.BytesToHash([]byte("def"))
	tkey := common.BytesToHash([]byte("jkl"))
	tval := common.BytesToHash([]byte("mno"))
	statedb.SetState(evmAddr, key, val)
	statedb.SetTransientState(evmAddr, tkey, tval)
	statedb.Ctx().EventManager().EmitEvent(sdk.Event{})

	rev := statedb.Snapshot()

	newVal := common.BytesToHash([]byte("x"))
	newTVal := common.BytesToHash([]byte("y"))
	statedb.SetState(evmAddr, key, newVal)
	statedb.SetTransientState(evmAddr, tkey, newTVal)
	statedb.Ctx().EventManager().EmitEvent(sdk.Event{})

	statedb.RevertToSnapshot(rev)

	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	require.Equal(t, val, statedb.GetState(evmAddr, key))

	newStateDB := state.NewDBImpl(ctx, k, false)
	// prev state DB not committed yet
	require.Equal(t, common.Hash{}, newStateDB.GetTransientState(evmAddr, tkey))
	require.Equal(t, common.Hash{}, newStateDB.GetState(evmAddr, key))

	_, err := statedb.Finalize()
	require.Nil(t, err)
	newStateDB = state.NewDBImpl(ctx, k, false)
	// prev state DB committed except for transient states
	require.Equal(t, common.Hash{}, newStateDB.GetTransientState(evmAddr, tkey))
	require.Equal(t, val, newStateDB.GetState(evmAddr, key))
	require.Equal(t, eventCount+1, len(ctx.EventManager().Events()))
}

// TestTransientStorageRevertNilMapPanic tests that reverting multiple transient storage
// changes does not panic when a prior revert deletes the account's transient state map.
func TestTransientStorageRevertNilMapPanic(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	_, evmAddr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)
	statedb.CreateAccount(evmAddr)

	tkey := common.BytesToHash([]byte("transient_key"))
	value1 := common.BytesToHash([]byte("value1"))
	value2 := common.BytesToHash([]byte("value2"))

	// Step 1: TSTORE(key, value1) - creates map, journal entry with prevalue=0
	statedb.SetTransientState(evmAddr, tkey, value1)
	require.Equal(t, value1, statedb.GetTransientState(evmAddr, tkey))

	// Step 2: Take first snapshot
	firstSnapshot := statedb.Snapshot()

	// Step 3: TSTORE(key, 0) - deletes value, journal entry with prevalue=value1
	statedb.SetTransientState(evmAddr, tkey, common.Hash{})
	require.Equal(t, common.Hash{}, statedb.GetTransientState(evmAddr, tkey))

	// Step 4: Take second snapshot (not used, but part of the sequence)
	_ = statedb.Snapshot()

	// Step 5: TSTORE(key, value2) - sets value, journal entry with prevalue=0
	statedb.SetTransientState(evmAddr, tkey, value2)
	require.Equal(t, value2, statedb.GetTransientState(evmAddr, tkey))

	// Step 6: RevertToSnapshot(first), this should NOT panic.
	require.NotPanics(t, func() {
		statedb.RevertToSnapshot(firstSnapshot)
	})

	// After revert, the transient state should be restored to value1
	require.Equal(t, value1, statedb.GetTransientState(evmAddr, tkey))
}

// TestGetCommittedState verifies that GetCommittedState returns the state from the
// underlying committed store, bypassing any cache layers from snapshots or uncommitted changes.
func TestGetCommittedState(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	_, evmAddr := testkeeper.MockAddressPair()

	key := common.BytesToHash([]byte("storage_key"))
	val1 := common.BytesToHash([]byte("value1"))
	val2 := common.BytesToHash([]byte("value2"))
	val3 := common.BytesToHash([]byte("value3"))

	// Test 1: GetCommittedState returns empty hash for non-existent state
	statedb := state.NewDBImpl(ctx, k, false)
	require.Equal(t, common.Hash{}, statedb.GetCommittedState(evmAddr, key))

	// Test 2: GetCommittedState returns empty hash for state set but not yet committed
	statedb.CreateAccount(evmAddr)
	statedb.SetState(evmAddr, key, val1)
	require.Equal(t, val1, statedb.GetState(evmAddr, key))
	require.Equal(t, common.Hash{}, statedb.GetCommittedState(evmAddr, key))

	// Test 3: After Finalize, GetCommittedState returns the committed value
	_, err := statedb.Finalize()
	require.NoError(t, err)
	ctx.MultiStore().(sdk.CacheMultiStore).Write()
	ctx.GigaMultiStore().WriteGiga()

	// Create a new statedb to verify the state was committed
	statedb2 := state.NewDBImpl(ctx, k, false)
	require.Equal(t, val1, statedb2.GetState(evmAddr, key))
	require.Equal(t, val1, statedb2.GetCommittedState(evmAddr, key))

	// Test 4: After modification, GetState returns new value but GetCommittedState returns old committed value
	statedb2.SetState(evmAddr, key, val2)
	require.Equal(t, val2, statedb2.GetState(evmAddr, key))
	require.Equal(t, val1, statedb2.GetCommittedState(evmAddr, key))

	// Test 5: After snapshot and further modifications, GetCommittedState still returns original committed value
	statedb2.Snapshot()
	statedb2.SetState(evmAddr, key, val3)
	require.Equal(t, val3, statedb2.GetState(evmAddr, key))
	require.Equal(t, val1, statedb2.GetCommittedState(evmAddr, key))

	// Test 6: After revert, GetState returns pre-snapshot value, GetCommittedState unchanged
	statedb2.RevertToSnapshot(0)
	require.Equal(t, val2, statedb2.GetState(evmAddr, key))
	require.Equal(t, val1, statedb2.GetCommittedState(evmAddr, key))

	// Test 7: Different keys have independent committed states
	key2 := common.BytesToHash([]byte("another_key"))
	statedb2.SetState(evmAddr, key2, val3)
	require.Equal(t, val3, statedb2.GetState(evmAddr, key2))
	require.Equal(t, common.Hash{}, statedb2.GetCommittedState(evmAddr, key2))

	// Test 8: Different addresses have independent committed states
	_, evmAddr2 := testkeeper.MockAddressPair()
	statedb2.CreateAccount(evmAddr2)
	statedb2.SetState(evmAddr2, key, val3)
	require.Equal(t, val3, statedb2.GetState(evmAddr2, key))
	require.Equal(t, common.Hash{}, statedb2.GetCommittedState(evmAddr2, key))
}

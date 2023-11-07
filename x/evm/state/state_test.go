package state_test

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestState(t *testing.T) {
	k, _, ctx := testkeeper.MockEVMKeeper()
	_, evmAddr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k)
	statedb.CreateAccount(evmAddr)
	require.True(t, statedb.Created(evmAddr))
	require.False(t, statedb.HasSelfDestructed(evmAddr))
	statedb.AddBalance(evmAddr, big.NewInt(10))
	k.BankKeeper().MintCoins(statedb.Ctx(), types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(10))))
	key := common.BytesToHash([]byte("abc"))
	val := common.BytesToHash([]byte("def"))
	statedb.SetState(evmAddr, key, val)
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
	// destruct should clear balance and state, but keep transient state. Committed state should also be accessible
	statedb.SelfDestruct(evmAddr)
	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	require.Equal(t, common.Hash{}, statedb.GetState(evmAddr, key))
	require.Equal(t, common.Hash{}, statedb.GetCommittedState(evmAddr, key))
	require.Equal(t, big.NewInt(0), statedb.GetBalance(evmAddr))
	require.True(t, statedb.HasSelfDestructed(evmAddr))
	// set storage
	statedb.SetStorage(evmAddr, map[common.Hash]common.Hash{{}: {}})
	require.Equal(t, common.Hash{}, statedb.GetState(evmAddr, common.Hash{}))
}

func TestCreate(t *testing.T) {
	k, _, ctx := testkeeper.MockEVMKeeper()
	_, evmAddr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k)
	statedb.CreateAccount(evmAddr)
	require.False(t, statedb.HasSelfDestructed(evmAddr))
	key := common.BytesToHash([]byte("abc"))
	val := common.BytesToHash([]byte("def"))
	tkey := common.BytesToHash([]byte("jkl"))
	tval := common.BytesToHash([]byte("mno"))
	statedb.SetState(evmAddr, key, val)
	statedb.SetTransientState(evmAddr, tkey, tval)
	statedb.AddBalance(evmAddr, big.NewInt(10000000000000))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(10))))
	// recreate an account should clear its state, but keep its balance and transient state
	statedb.CreateAccount(evmAddr)
	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	require.Equal(t, common.Hash{}, statedb.GetState(evmAddr, key))
	require.Equal(t, big.NewInt(10000000000000), statedb.GetBalance(evmAddr))
	require.True(t, statedb.Created(evmAddr))
	require.False(t, statedb.HasSelfDestructed(evmAddr))
	// recreate a destructed (in the same tx) account should clear its selfDestructed flag
	statedb.SelfDestruct(evmAddr)
	require.Nil(t, statedb.Err())
	require.True(t, statedb.HasSelfDestructed(evmAddr))
	require.Equal(t, big.NewInt(0), statedb.GetBalance(evmAddr))
	statedb.CreateAccount(evmAddr)
	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	require.Equal(t, common.Hash{}, statedb.GetState(evmAddr, key))
	require.Equal(t, big.NewInt(0), statedb.GetBalance(evmAddr)) // cleared during SelfDestruct
	require.True(t, statedb.Created(evmAddr))
	require.False(t, statedb.HasSelfDestructed(evmAddr))
}

func TestSelfDestructAssociated(t *testing.T) {
	k, _, ctx := testkeeper.MockEVMKeeper()
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	statedb := state.NewDBImpl(ctx, k)
	statedb.CreateAccount(evmAddr)
	key := common.BytesToHash([]byte("abc"))
	val := common.BytesToHash([]byte("def"))
	tkey := common.BytesToHash([]byte("jkl"))
	tval := common.BytesToHash([]byte("mno"))
	statedb.SetState(evmAddr, key, val)
	statedb.SetTransientState(evmAddr, tkey, tval)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(10)))
	k.BankKeeper().MintCoins(statedb.Ctx(), types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(statedb.Ctx(), types.ModuleName, seiAddr, amt)

	// Selfdestruct6780 should only act if the account is created in the same block
	statedb.MarkAccount(evmAddr, nil)
	statedb.Selfdestruct6780(evmAddr)
	require.Equal(t, val, statedb.GetState(evmAddr, key))
	statedb.MarkAccount(evmAddr, state.AccountCreated)
	require.False(t, statedb.HasSelfDestructed(evmAddr))

	// Selfdestruct6780 is equivalent to SelfDestruct if account is created in the same block
	statedb.Selfdestruct6780(evmAddr)
	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	require.Equal(t, common.Hash{}, statedb.GetState(evmAddr, key))
	require.Equal(t, big.NewInt(0), statedb.GetBalance(evmAddr))
	require.Equal(t, big.NewInt(0), k.BankKeeper().GetBalance(ctx, seiAddr, k.GetBaseDenom(ctx)).Amount.BigInt())
	require.True(t, statedb.HasSelfDestructed(evmAddr))
	require.False(t, statedb.Created(evmAddr))
	// association should also be removed
	_, ok := k.GetSeiAddress(statedb.Ctx(), evmAddr)
	require.False(t, ok)
}

func TestSnapshot(t *testing.T) {
	k, _, ctx := testkeeper.MockEVMKeeper()
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	statedb := state.NewDBImpl(ctx, k)
	statedb.CreateAccount(evmAddr)
	key := common.BytesToHash([]byte("abc"))
	val := common.BytesToHash([]byte("def"))
	tkey := common.BytesToHash([]byte("jkl"))
	tval := common.BytesToHash([]byte("mno"))
	statedb.SetState(evmAddr, key, val)
	statedb.SetTransientState(evmAddr, tkey, tval)

	rev := statedb.Snapshot()

	newVal := common.BytesToHash([]byte("x"))
	newTVal := common.BytesToHash([]byte("y"))
	statedb.SetState(evmAddr, key, newVal)
	statedb.SetTransientState(evmAddr, tkey, newTVal)

	statedb.RevertToSnapshot(rev)

	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	require.Equal(t, val, statedb.GetState(evmAddr, key))

	newStateDB := state.NewDBImpl(ctx, k)
	// prev state DB not committed yet
	require.Equal(t, common.Hash{}, newStateDB.GetTransientState(evmAddr, tkey))
	require.Equal(t, common.Hash{}, newStateDB.GetState(evmAddr, key))

	require.Nil(t, statedb.Finalize())
	newStateDB = state.NewDBImpl(ctx, k)
	// prev state DB committed except for transient states
	require.Equal(t, common.Hash{}, newStateDB.GetTransientState(evmAddr, tkey))
	require.Equal(t, val, newStateDB.GetState(evmAddr, key))
}

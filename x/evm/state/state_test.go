package state

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestState(t *testing.T) {
	k, _, ctx := keeper.MockEVMKeeper()
	_, evmAddr := keeper.MockAddressPair()
	statedb := NewStateDBImpl(ctx, k)
	statedb.CreateAccount(evmAddr)
	require.Contains(t, statedb.created, evmAddr.String())
	require.NotContains(t, statedb.selfDestructedAccs, evmAddr.String())
	statedb.AddBalance(evmAddr, big.NewInt(10))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(10))))
	// set state to the committed store first
	key := common.BytesToHash([]byte("abc"))
	val := common.BytesToHash([]byte("def"))
	statedb.SetState(evmAddr, key, val)
	require.Equal(t, val, statedb.GetState(evmAddr, key))
	require.Equal(t, val, statedb.GetCommittedState(evmAddr, key))
	// fork the store and overwrite the key
	statedb.ctx = ctx.WithMultiStore(ctx.MultiStore().CacheMultiStore())
	newVal := common.BytesToHash([]byte("ghi"))
	statedb.SetState(evmAddr, key, newVal)
	require.Equal(t, newVal, statedb.GetState(evmAddr, key))
	require.Equal(t, val, statedb.GetCommittedState(evmAddr, key))
	tkey := common.BytesToHash([]byte("jkl"))
	tval := common.BytesToHash([]byte("mno"))
	statedb.SetTransientState(evmAddr, tkey, tval)
	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	// destruct should clear balance and state, but keep transient state. Committed state should also be accessible
	statedb.SelfDestruct(evmAddr)
	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	require.Equal(t, common.Hash{}, statedb.GetState(evmAddr, key))
	require.Equal(t, val, statedb.GetCommittedState(evmAddr, key))
	require.Equal(t, big.NewInt(0), statedb.GetBalance(evmAddr))
}

func TestCreate(t *testing.T) {
	k, _, ctx := keeper.MockEVMKeeper()
	_, evmAddr := keeper.MockAddressPair()
	statedb := NewStateDBImpl(ctx, k)
	statedb.CreateAccount(evmAddr)
	key := common.BytesToHash([]byte("abc"))
	val := common.BytesToHash([]byte("def"))
	tkey := common.BytesToHash([]byte("jkl"))
	tval := common.BytesToHash([]byte("mno"))
	statedb.SetState(evmAddr, key, val)
	statedb.SetTransientState(evmAddr, tkey, tval)
	statedb.AddBalance(evmAddr, big.NewInt(10))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(10))))
	// recreate an account should clear its state, but keep its balance and transient state
	statedb.CreateAccount(evmAddr)
	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	require.Equal(t, common.Hash{}, statedb.GetState(evmAddr, key))
	require.Equal(t, big.NewInt(10), statedb.GetBalance(evmAddr))
	require.Contains(t, statedb.created, evmAddr.String())
	require.NotContains(t, statedb.selfDestructedAccs, evmAddr.String())
	// recreate a destructed (in the same tx) account should clear its selfDestructed flag
	statedb.SelfDestruct(evmAddr)
	require.Equal(t, big.NewInt(0), statedb.GetBalance(evmAddr))
	statedb.CreateAccount(evmAddr)
	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	require.Equal(t, common.Hash{}, statedb.GetState(evmAddr, key))
	require.Equal(t, big.NewInt(0), statedb.GetBalance(evmAddr)) // cleared during SelfDestruct
	require.Contains(t, statedb.created, evmAddr.String())
	require.NotContains(t, statedb.selfDestructedAccs, evmAddr.String())
}

func TestSelfDestructAssociated(t *testing.T) {
	k, _, ctx := keeper.MockEVMKeeper()
	seiAddr, evmAddr := keeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	statedb := NewStateDBImpl(ctx, k)
	statedb.CreateAccount(evmAddr)
	key := common.BytesToHash([]byte("abc"))
	val := common.BytesToHash([]byte("def"))
	tkey := common.BytesToHash([]byte("jkl"))
	tval := common.BytesToHash([]byte("mno"))
	statedb.SetState(evmAddr, key, val)
	statedb.SetTransientState(evmAddr, tkey, tval)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(10)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, seiAddr, amt)

	// SelfDestruct6780 should only act if the account is created in the same block
	tmp := statedb.created
	statedb.created = map[string]struct{}{}
	statedb.SelfDestruct6780(evmAddr)
	require.Equal(t, val, statedb.GetState(evmAddr, key))
	statedb.created = tmp

	// SelfDestruct6780 is equivalent to SelfDestruct if account is created in the same block
	statedb.SelfDestruct6780(evmAddr)
	require.Equal(t, tval, statedb.GetTransientState(evmAddr, tkey))
	require.Equal(t, common.Hash{}, statedb.GetState(evmAddr, key))
	require.Equal(t, big.NewInt(0), statedb.GetBalance(evmAddr))
	require.Equal(t, big.NewInt(0), k.BankKeeper().GetBalance(ctx, seiAddr, k.GetBaseDenom(ctx)).Amount.BigInt())
	// association should also be removed
	_, ok := k.GetSeiAddress(ctx, evmAddr)
	require.False(t, ok)
}

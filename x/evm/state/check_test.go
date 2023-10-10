package state_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestExist(t *testing.T) {
	// not exist
	k, _, ctx := keeper.MockEVMKeeper()
	_, addr := keeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k)
	require.False(t, statedb.Exist(addr))

	// has state
	statedb.SetState(addr, common.BytesToHash([]byte{1}), common.BytesToHash([]byte{2}))
	require.True(t, statedb.Exist(addr))

	// has code
	_, addr2 := keeper.MockAddressPair()
	statedb.SetCode(addr2, []byte{3})
	require.True(t, statedb.Exist(addr2))

	// destructed
	_, addr3 := keeper.MockAddressPair()
	statedb.SelfDestruct(addr3)
	require.True(t, statedb.Exist(addr3))
}

func TestEmpty(t *testing.T) {
	// empty
	k, _, ctx := keeper.MockEVMKeeper()
	_, addr := keeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k)
	require.True(t, statedb.Empty(addr))

	// has balance
	statedb.AddBalance(addr, big.NewInt(1))
	require.False(t, statedb.Empty(addr))

	// has non-zero nonce
	statedb.SubBalance(addr, big.NewInt(1))
	statedb.SetNonce(addr, 1)
	require.False(t, statedb.Empty(addr))

	// has code
	statedb.SetNonce(addr, 0)
	statedb.SetCode(addr, []byte{1})
	require.False(t, statedb.Empty(addr))
}

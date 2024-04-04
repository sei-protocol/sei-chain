package state_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/tracing"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestExist(t *testing.T) {
	// not exist
	k, ctx := testkeeper.MockEVMKeeper()
	_, addr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)
	require.False(t, statedb.Exist(addr))

	// has code
	_, addr2 := testkeeper.MockAddressPair()
	statedb.SetCode(addr2, []byte{3})
	require.True(t, statedb.Exist(addr2))

	// has balance
	_, addr3 := testkeeper.MockAddressPair()
	statedb.AddBalance(addr3, big.NewInt(1000000000000))
	require.True(t, statedb.Exist(addr3))

	// destructed
	_, addr4 := testkeeper.MockAddressPair()
	statedb.SelfDestruct(addr4)
	require.True(t, statedb.Exist(addr4))
}

func TestEmpty(t *testing.T) {
	// empty
	k, ctx := testkeeper.MockEVMKeeper()
	_, addr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)
	require.True(t, statedb.Empty(addr))

	// has balance
	statedb.AddBalance(addr, big.NewInt(1000000000000), tracing.BalanceChangeUnspecified)
	require.False(t, statedb.Empty(addr))

	// has non-zero nonce
	statedb.SubBalance(addr, big.NewInt(1000000000000), tracing.BalanceChangeUnspecified)
	statedb.SetNonce(addr, 1)
	require.False(t, statedb.Empty(addr))

	// has code
	statedb.SetNonce(addr, 0)
	statedb.SetCode(addr, []byte{1})
	require.False(t, statedb.Empty(addr))
}

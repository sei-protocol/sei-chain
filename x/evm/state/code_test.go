package state_test

import (
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestCode(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, addr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)

	require.Equal(t, ethtypes.EmptyCodeHash, statedb.GetCodeHash(addr))
	require.Nil(t, statedb.GetCode(addr))
	require.Equal(t, 0, statedb.GetCodeSize(addr))

	code := []byte{1, 2, 3, 4, 5}
	statedb.SetCode(addr, code)
	require.Equal(t, crypto.Keccak256Hash(code), statedb.GetCodeHash(addr))
	require.Equal(t, code, statedb.GetCode(addr))
	require.Equal(t, 5, statedb.GetCodeSize(addr))
}

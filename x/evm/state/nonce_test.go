package state_test

import (
	"github.com/stretchr/testify/require"
	"testing"

	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

func TestNonce(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	stateDB := state.NewDBImpl(ctx, k, false)
	_, addr := testkeeper.MockAddressPair()
	stateDB.SetNonce(addr, 1)
	nonce := stateDB.GetNonce(addr)
	require.Equal(t, nonce, uint64(1))
	stateDB.SetNonce(addr, 2)
	nonce = stateDB.GetNonce(addr)
	require.Equal(t, nonce, uint64(2))
}

package state_test

import (
	"testing"
	"time"

	testkeeper "github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
	"github.com/stretchr/testify/require"
)

func TestGasRefund(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	statedb := state.NewDBImpl(ctx, k, false)

	require.Equal(t, uint64(0), statedb.GetRefund())
	statedb.AddRefund(2)
	require.Equal(t, uint64(2), statedb.GetRefund())
	statedb.SubRefund(1)
	require.Equal(t, uint64(1), statedb.GetRefund())
	require.Panics(t, func() { statedb.SubRefund(2) })
}

package state_test

import (
	"testing"
	"time"

	"github.com/holiman/uint256"
	testkeeper "github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
	"github.com/stretchr/testify/require"
)

func TestEventlessTransfer(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	db := state.NewDBImpl(ctx, k, false)
	_, fromAddr := testkeeper.MockAddressPair()
	_, toAddr := testkeeper.MockAddressPair()

	beforeLen := len(ctx.EventManager().ABCIEvents())

	state.TransferWithoutEvents(db, fromAddr, toAddr, uint256.NewInt(100))

	// should be unchanged
	require.Len(t, ctx.EventManager().ABCIEvents(), beforeLen)
}

package state_test

import (
	"testing"
	"time"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestEventlessTransfer(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	db := state.NewDBImpl(ctx, k, false)
	_, fromAddr := testkeeper.MockAddressPair()
	_, toAddr := testkeeper.MockAddressPair()

	beforeLen := len(ctx.EventManager().ABCIEvents())

	rules := types.EthereumRules(ctx.BlockTime().Unix())
	state.TransferWithoutEvents(db, fromAddr, toAddr, uint256.NewInt(100), &rules)

	// should be unchanged
	require.Len(t, ctx.EventManager().ABCIEvents(), beforeLen)
	// the EIP-7708 log is an EVM log, not a Cosmos event
	want := ethtypes.EthTransferLog(fromAddr, toAddr, uint256.NewInt(100))
	logs := db.GetAllLogs()
	require.Len(t, logs, 1)
	require.Equal(t, want.Address, logs[0].Address)
	require.Equal(t, want.Topics, logs[0].Topics)
	require.Equal(t, want.Data, logs[0].Data)
}

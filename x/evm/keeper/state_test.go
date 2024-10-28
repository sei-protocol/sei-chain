package keeper_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestState(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, addr := testkeeper.MockAddressPair()

	initialState := k.GetState(ctx, addr, common.HexToHash("0xabc"))
	require.Equal(t, common.Hash{}, initialState)
	k.SetState(ctx, addr, common.HexToHash("0xabc"), common.HexToHash("0xdef"))

	got := k.GetState(ctx, addr, common.HexToHash("0xabc"))
	require.Equal(t, common.HexToHash("0xdef"), got)
}

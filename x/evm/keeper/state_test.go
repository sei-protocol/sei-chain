package keeper_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
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

	store := k.PrefixStore(ctx, types.StateKey(addr))
	key := common.HexToHash("0xabc")
	require.True(t, store.Has(key[:]))
	k.SetState(ctx, addr, key, common.Hash{})
	require.Equal(t, common.Hash{}, k.GetState(ctx, addr, key))
	require.False(t, store.Has(key[:]))
}

package keeper

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestState(t *testing.T) {
	k, _, ctx := MockEVMKeeper()
	_, addr := MockAddressPair()

	initialState := k.GetState(ctx, addr, common.HexToHash("0xabc"))
	require.Equal(t, common.Hash{}, initialState)
	k.SetState(ctx, addr, common.HexToHash("0xabc"), common.HexToHash("0xdef"))

	got := k.GetState(ctx, addr, common.HexToHash("0xabc"))
	require.Equal(t, common.HexToHash("0xdef"), got)
}

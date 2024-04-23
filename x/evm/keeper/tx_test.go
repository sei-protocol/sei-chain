package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestTxHashesOnHeight(t *testing.T) {
	k, ctx := keepertest.MockEVMKeeper()
	require.Empty(t, k.GetTxHashesOnHeight(ctx, 1234))
	hashes := []common.Hash{
		common.HexToHash("0x0750333eac0be1203864220893d8080dd8a8fd7a2ed098dfd92a718c99d437f2"),
		common.HexToHash("0x6f0c1476adb51b1646ff35433b410f1e9c326bd6428f90acf39d0bb1a312bc50"),
	}
	k.SetTxHashesOnHeight(ctx, 1234, hashes)
	require.Equal(t, hashes, k.GetTxHashesOnHeight(ctx, 1234))
}

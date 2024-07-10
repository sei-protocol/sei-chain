package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestReceipt(t *testing.T) {
	k, ctx := keepertest.MockEVMKeeper()
	txHash := common.HexToHash("0x0750333eac0be1203864220893d8080dd8a8fd7a2ed098dfd92a718c99d437f2")
	_, err := k.GetReceipt(ctx, txHash)
	require.NotNil(t, err)
	k.MockReceipt(ctx, txHash, &types.Receipt{TxHashHex: txHash.Hex()})
	r, err := k.GetReceipt(ctx, txHash)
	require.Nil(t, err)
	require.Equal(t, txHash.Hex(), r.TxHashHex)
}

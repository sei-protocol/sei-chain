package tests

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/evmrpc"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestGasPriceCongestionThreshold(t *testing.T) {
	// Verify threshold uses > 80% (strictly greater). At exactly 80% → not congested
	// Above 80% → congested.

	// Create a ctx provider with explicit MaxGas to make threshold deterministic
	baseCtx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	consensusParams := &tmproto.ConsensusParams{Block: &tmproto.BlockParams{MaxGas: 10000000}}
	baseCtx = baseCtx.WithConsensusParams(consensusParams)
	ctxProvider := func(int64) sdk.Context { return baseCtx }

	i := evmrpc.NewInfoAPI(nil, nil, ctxProvider, nil, t.TempDir(), 1024, evmrpc.ConnectionTypeHTTP, nil)

	oneGwei := big.NewInt(1000000000)
	median := big.NewInt(2000000000) // 2 gwei

	// Exactly at 80% → not congested → 10% bump on base fee
	gasPrice, err := i.GasPriceHelper(
		baseCtx.Context(),
		oneGwei,
		8000000, // 80% of 10,000,000
		median,
	)
	require.NoError(t, err)
	// expected = baseFee * 110 / 100
	expectedNotCongested := new(big.Int).Mul(new(big.Int).Set(oneGwei), big.NewInt(110))
	expectedNotCongested.Div(expectedNotCongested, big.NewInt(100))
	require.Equal(t, expectedNotCongested, gasPrice.ToInt())

	// Just above 80% → congested → baseFee + median
	gasPrice, err = i.GasPriceHelper(
		baseCtx.Context(),
		oneGwei,
		8000001,
		median,
	)
	require.NoError(t, err)
	require.Equal(t, new(big.Int).Add(oneGwei, median), gasPrice.ToInt())
}

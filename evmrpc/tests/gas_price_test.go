package tests

import (
	"math/big"
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/evmrpc"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestGasPriceCongestionThreshold(t *testing.T) {
	// Verify threshold uses > 80% (strictly greater). At exactly 80% → not congested
	// Above 80% → congested.

	// Create a ctx provider with explicit MaxGas to make threshold deterministic
	baseCtx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	consensusParams := &tmproto.ConsensusParams{Block: &tmproto.BlockParams{MaxGas: 10000000}}
	baseCtx = baseCtx.WithConsensusParams(consensusParams)
	ctxProvider := func(int64) sdk.Context { return baseCtx }

	i := evmrpc.NewInfoAPI(nil, nil, ctxProvider, nil, t.TempDir(), 1024, evmrpc.ConnectionTypeHTTP, nil, nil)

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

func TestGasPriceInvokesFeeHistoryMedian(t *testing.T) {
	// Single empty block is sufficient; FeeHistory will still execute and return zeros
	// and GasPrice will compute based on baseFee without error.
	SetupMockPacificTestServer(t, func(app *app.App, _ *MockClient) sdk.Context {
		// height can be any valid block height
		ctx := app.RPCContextProvider(evmrpc.LatestCtxHeight).WithClosestUpgradeName("pacific-1")
		cp := &tmproto.ConsensusParams{Block: &tmproto.BlockParams{MaxGas: 10_000_000}}
		return ctx.WithConsensusParams(cp)
	}).Run(func(port int) {
		res := sendRequestWithNamespace("eth", port, "gasPrice")

		// Ensure no top-level error is returned by the RPC
		if errObj, hasErr := res["error"]; hasErr {
			t.Fatalf("eth_gasPrice returned error: %v", errObj)
		}

		// Result should be a hex string; validate it's a positive integer
		result, ok := res["result"].(string)
		require.True(t, ok, "result should be a hex string")
		n := new(big.Int)
		_, ok = n.SetString(result, 0)
		require.True(t, ok, "result should parse as hex big.Int")
		require.NotEqual(t, 0, n.Sign(), "gas price should be non-zero")
	})
}

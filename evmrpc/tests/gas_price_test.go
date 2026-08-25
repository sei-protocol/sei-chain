package tests

import (
	"math/big"
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/evmrpc"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

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

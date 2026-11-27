package tests

import (
	"math/big"
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

// maxGasInitializer sets the chain's MaxGas consensus parameter to maxGas.
func maxGasInitializer(maxGas int64) func(sdk.Context, *app.App) {
	return func(ctx sdk.Context, a *app.App) {
		cp := *a.GetConsensusParams(ctx)
		block := *cp.Block
		block.MaxGas = maxGas
		cp.Block = &block
		a.StoreConsensusParams(ctx, &cp)
	}
}

// sendWithGasLimit returns a transfer like send's, declaring gasLimit.
func sendWithGasLimit(nonce, gasLimit uint64) ethtypes.TxData {
	_, recipient := testkeeper.MockAddressPair()
	return &ethtypes.DynamicFeeTx{
		Nonce:     nonce,
		GasFeeCap: big.NewInt(1000000000),
		Gas:       gasLimit,
		To:        &recipient,
		Value:     big.NewInt(2000),
		ChainID:   chainId,
	}
}

// TestTraceSkipsTxRefusedForBlockGas checks that replaying a block leaves out a tx the block refused
// for block gas, so a later tx spending the nonce it left unspent traces as it executed.
func TestTraceSkipsTxRefusedForBlockGas(t *testing.T) {
	// Of 50_000 gas, the first transfer leaves 29_000: too little for the second's 30_000 limit,
	// enough for the third's 21_000.
	executedTx := signAndEncodeTx(send(0), mnemonic1)
	refusedTx := signAndEncodeTx(sendWithGasLimit(1, 30_000), mnemonic1)
	reusedNonceData := send(1)
	reusedNonce := signTxWithMnemonic(reusedNonceData, mnemonic1)
	reusedNonceTx := encodeEvmTx(reusedNonceData, reusedNonce)

	ts := SetupTestServer(t, [][][]byte{{executedTx, refusedTx, reusedNonceTx}},
		mnemonicInitializer(mnemonic1), maxGasInitializer(50_000))
	refused := ts.mockClient.txResults[1][1]
	require.Equal(t, types.ErrBlockGasLimitReached.ABCICode(), refused.Code, refused.Log)
	ts.RequireTxSucceeded(t, 2, 2)

	ts.Run(func(port int) {
		traceGasUsed := func(trace map[string]interface{}) interface{} {
			t.Helper()
			require.NotContains(t, trace, "error")
			frame := trace["result"].([]interface{})[0].(map[string]interface{})
			require.NotContains(t, frame, "error")
			return frame["result"].(map[string]interface{})["gasUsed"]
		}

		blockRes := sendRequestWithNamespace("debug", port, "traceBlockByNumber", "0x2", map[string]interface{}{
			"timeout": "60s", "tracer": "flatCallTracer",
		})
		require.NotContains(t, blockRes, "error")
		traced := blockRes["result"].([]interface{})
		require.Len(t, traced, 2, "the refused tx has no receipt, so the block trace leaves it out")
		require.Equal(t, "0x5208", traceGasUsed(traced[1].(map[string]interface{})))

		txRes := sendRequestWithNamespace("debug", port, "traceTransaction", reusedNonce.Hash().Hex(), map[string]interface{}{
			"timeout": "60s", "tracer": "flatCallTracer",
		})
		require.Equal(t, "0x5208", traceGasUsed(txRes))
	})
}

package tests

import (
	"encoding/json"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// eth_call, eth_estimateGas and debug_traceCall at a block run at that block's own base fee, the one
// eth_getBlockByNumber reports for it.
func TestCallUsesBlockBaseFee(t *testing.T) {
	// Setup: blocks 2 and 3 each carry a transfer above the target gas, so the base fee rises after each of them.
	recipient := common.HexToAddress("0x00000000000000000000000000000000000a0005")
	reader := common.HexToAddress("0x00000000000000000000000000000000000a0006")
	fund := func(nonce uint64) []byte {
		return signAndEncodeTx(&ethtypes.DynamicFeeTx{
			Nonce:     nonce,
			GasFeeCap: big.NewInt(100000000000),
			Gas:       21000,
			To:        &recipient,
			Value:     big.NewInt(1),
			ChainID:   chainId,
		}, mnemonic1)
	}
	risingBaseFee := func(ctx sdk.Context, a *app.App) {
		a.EvmKeeper.SetNextBaseFeePerGas(ctx, sdk.NewDec(50000000000))
		params := a.EvmKeeper.GetParams(ctx)
		params.TargetGasUsedPerBlock = 1
		a.EvmKeeper.SetParams(ctx, params)
	}
	ts := SetupTestServer(t, [][][]byte{{fund(0)}, {fund(1)}}, mnemonicInitializer(mnemonic1), risingBaseFee)
	ts.Run(func(port int) {
		ts.RequireTxSucceeded(t, 2, 0)
		ts.RequireTxSucceeded(t, 3, 0)
		blockBaseFee := func(block string) *big.Int {
			var blk struct {
				BaseFeePerGas *hexutil.Big `json:"baseFeePerGas"`
			}
			require.NoError(t, json.Unmarshal(requireRPCResult(t, port, "eth_getBlockByNumber", block, false), &blk))
			require.NotNil(t, blk.BaseFeePerGas, "block %s", block)
			return blk.BaseFeePerGas.ToInt()
		}
		require.Equal(t, -1, blockBaseFee("0x2").Cmp(blockBaseFee("latest")), "base fee must rise between blocks for this test to discriminate")

		for _, block := range []string{"0x2", "latest"} {
			want := blockBaseFee(block)
			// A gas price equal to the block's base fee is below every later block's.
			args := map[string]any{
				"from":     mnemonic1Addr.Hex(),
				"to":       reader.Hex(),
				"gasPrice": hexutil.EncodeBig(want),
			}
			// BASEFEE PUSH1 0 MSTORE PUSH1 32 PUSH1 0 RETURN
			returnBaseFee := map[string]any{reader.Hex(): map[string]any{"code": "0x4860005260206000f3"}}

			// Test: eth_call returns BASEFEE.
			var out hexutil.Bytes
			require.NoError(t, json.Unmarshal(requireRPCResult(t, port, "eth_call", args, block, returnBaseFee), &out))

			// Verify: it is the block's own base fee.
			require.Equal(t, want, new(big.Int).SetBytes(out), "eth_call at %s", block)

			// Test: debug_traceCall returns BASEFEE.
			var trace struct {
				Output hexutil.Bytes `json:"output"`
			}
			require.NoError(t, json.Unmarshal(requireRPCResult(t, port, "debug_traceCall", args, block,
				map[string]any{"tracer": "callTracer", "stateOverrides": returnBaseFee}), &trace))

			// Verify: it is the same base fee eth_call saw.
			require.Equal(t, want, new(big.Int).SetBytes(trace.Output), "debug_traceCall at %s", block)

			// Test: eth_estimateGas on code that reverts unless BASEFEE is the block's own.
			// BASEFEE PUSH32 want EQ PUSH1 0x2a JUMPI PUSH1 0 DUP1 REVERT JUMPDEST STOP
			requireBaseFee := map[string]any{reader.Hex(): map[string]any{
				"code": fmt.Sprintf("0x487f%064x14602a57600080fd5b00", want),
			}}

			// Verify: the estimate succeeds, so the code saw that base fee.
			requireRPCResult(t, port, "eth_estimateGas", args, block, requireBaseFee)
		}
	})
}

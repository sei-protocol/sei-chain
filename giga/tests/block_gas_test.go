package giga_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

// withMaxGas returns ctx with its consensus params' MaxGas set to maxGas.
func withMaxGas(ctx sdk.Context, maxGas int64) sdk.Context {
	cp := &tmproto.ConsensusParams{}
	if existing := ctx.ConsensusParams(); existing != nil {
		copied := *existing
		cp = &copied
	}
	block := tmproto.BlockParams{}
	if cp.Block != nil {
		block = *cp.Block
	}
	block.MaxGas = maxGas
	cp.Block = &block
	return ctx.WithConsensusParams(cp)
}

// evmTxHash returns the hash of the EVM tx wrapped in txBytes.
func evmTxHash(t *testing.T, txBytes []byte) common.Hash {
	t.Helper()
	tx, err := app.MakeEncodingConfig().TxConfig.TxDecoder()(txBytes)
	require.NoError(t, err)
	etx, _ := types.MustGetEVMTransactionMessage(tx).AsTransaction()
	require.NotNil(t, etx)
	return etx.Hash()
}

// TestAllModes_BlockGasLimit runs a block whose EVM gas limits sum past MaxGas through every
// executor mode, and checks that each executes the txs that fit and records the rest, identically
// and without executing them.
func TestAllModes_BlockGasLimit(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)
	// Four 21_000-gas transfers against 50_000 gas: two execute, and then 8_000 gas is left.
	transfers := GenerateNonConflictingTransfers(4)
	const executedTxs = 2

	var baseline []*abci.ExecTxResult
	var baselineCtx *GigaTestContext
	for _, mode := range []ExecutorMode{ModeV2Sequential, ModeV2withOCC, ModeGigaSequential, ModeGigaOCC} {
		t.Run(mode.String(), func(t *testing.T) {
			tCtx := NewGigaTestContext(t, accts, blockTime, 4, mode)
			txs := CreateEVMTransferTxs(t, tCtx, transfers, mode == ModeGigaSequential || mode == ModeGigaOCC)
			tCtx.Ctx = withMaxGas(tCtx.Ctx, 50_000)

			_, results, err := RunBlock(t, tCtx, txs)
			require.NoError(t, err)
			require.Len(t, results, len(txs))
			for i, res := range results {
				sender := transfers[i].Signer.EvmAddress
				if i < executedTxs {
					require.Equal(t, uint32(0), res.Code, "tx[%d]: %s", i, res.Log)
					require.Equal(t, uint64(1), tCtx.TestApp.EvmKeeper.GetNonce(tCtx.Ctx, sender), "tx[%d]", i)
					continue
				}
				require.Equal(t, types.ModuleName, res.Codespace, "tx[%d]", i)
				require.Equal(t, types.ErrBlockGasLimitReached.ABCICode(), res.Code, "tx[%d]: %s", i, res.Log)
				require.Zero(t, res.GasUsed, "tx[%d]", i)
				require.Nil(t, res.EvmTxInfo, "tx[%d]", i)
				_, err := tCtx.GetTransientReceipt(t, evmTxHash(t, txs[i]), uint64(i)) //nolint:gosec // i is a slice index
				require.Error(t, err, "tx[%d] has no receipt", i)
				require.Zero(t, tCtx.TestApp.EvmKeeper.GetNonce(tCtx.Ctx, sender), "tx[%d] leaves its nonce unspent", i)
			}

			if baseline == nil {
				baseline, baselineCtx = results, tCtx
				return
			}
			CompareResults(t, "BlockGasLimit_"+mode.String(), baseline, results)
			CompareReceipts(t, "BlockGasLimit_"+mode.String(), baselineCtx, baseline, tCtx, results)
		})
	}
}

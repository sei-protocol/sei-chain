package mempool

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// evmWrappedTx returns an EVM tx from sender with the given gas wanted and estimated.
func evmWrappedTx(sender common.Address, wanted, estimated int64) *WrappedTx {
	return &WrappedTx{gasWanted: wanted, estimatedGas: estimated, evm: utils.Some(evmTx{address: sender})}
}

// cosmosWrappedTx returns a non-EVM tx with the given gas wanted.
func cosmosWrappedTx(wanted int64) *WrappedTx {
	return &WrappedTx{gasWanted: wanted, estimatedGas: wanted}
}

func TestReapGasFitsAnEVMTxByItsGasWantedAndChargesItsEstimate(t *testing.T) {
	sender := common.HexToAddress("0x01")
	gas := reapGas{maxWanted: utils.Max[int64](), maxEstimated: 100_000, evmSlack: utils.Max[int64]()}

	require.True(t, gas.add(evmWrappedTx(sender, 90_000, 30_000)))
	require.False(t, gas.add(evmWrappedTx(sender, 80_000, 21_000)), "80_000 wanted exceeds the 70_000 left")
	require.True(t, gas.add(evmWrappedTx(sender, 70_000, 21_000)))
	require.Equal(t, int64(51_000), gas.estimated)
}

func TestReapGasKeepsCosmosTxsFromShrinkingAReapedEVMTx(t *testing.T) {
	sender := common.HexToAddress("0x01")
	gas := reapGas{maxWanted: utils.Max[int64](), maxEstimated: 100_000, evmSlack: utils.Max[int64]()}

	require.True(t, gas.add(evmWrappedTx(sender, 90_000, 21_000)), "leaves 10_000 to spare")
	require.False(t, gas.add(cosmosWrappedTx(20_000)), "would leave the EVM tx 80_000 of the 90_000 it wants")
	require.True(t, gas.add(cosmosWrappedTx(10_000)))
	require.False(t, gas.add(cosmosWrappedTx(1)))
	require.True(t, gas.add(evmWrappedTx(sender, 69_000, 21_000)), "a later EVM tx sees what the Cosmos tx reserved")
}

func TestReapGasHoldsGasWantedToMaxWanted(t *testing.T) {
	sender := common.HexToAddress("0x01")
	gas := reapGas{maxWanted: 100_000, maxEstimated: utils.Max[int64](), evmSlack: utils.Max[int64]()}

	require.True(t, gas.add(evmWrappedTx(sender, 60_000, 21_000)))
	require.False(t, gas.add(evmWrappedTx(sender, 50_000, 21_000)))
	require.True(t, gas.add(cosmosWrappedTx(40_000)))
	require.Equal(t, int64(100_000), gas.wanted)
}

func TestTxMempool_ReapSkipsTxsWhoseGasDoesNotFit(t *testing.T) {
	ctx := t.Context()

	addrA := "0x00000000000000000000000000000000000000aa"
	addrB := "0x00000000000000000000000000000000000000bb"
	addrC := "0x00000000000000000000000000000000000000cc"
	evmTx := func(addr string, priority int64, nonce uint64) string {
		return fmt.Sprintf("evm-sender=%s=%d=%d", addr, priority, nonce)
	}
	a0 := evmTx(addrA, 9, 0)
	b0, b1 := evmTx(addrB, 8, 0), evmTx(addrB, 8, 1)
	c0 := evmTx(addrC, 7, 0)
	bigCosmos, smallCosmos := "sender-0=peer=6", "sender-1=peer=5"
	gasOf := map[string][2]int64{
		a0:          {90_000, 30_000}, // fits, then uses 30_000 and leaves 10_000 to spare
		b0:          {80_000, 21_000}, // wants more than the 70_000 left
		b1:          {21_000, 21_000}, // fits, but follows its sender's skipped tx
		c0:          {50_000, 21_000}, // fits in the 70_000 left
		bigCosmos:   {15_000, 0},      // would leave a0 short
		smallCosmos: {10_000, 0},      // fits in what a0 spares
	}
	client := &application{Application: kvstore.NewApplication(), gasOf: func(tx []byte) (int64, int64) {
		gas := gasOf[string(tx)]
		return gas[0], gas[1]
	}}
	cfg := TestConfig()
	cfg.CacheSize = 0
	txmp := setup(cfg, proxy.New(client), NopTxConstraintsFetcher)
	for _, tx := range []string{a0, b0, b1, c0, bigCosmos, smallCosmos} {
		_, err := txmp.CheckTx(ctx, []byte(tx))
		require.NoError(t, err)
	}

	reaped, _ := txmp.ReapTxs(ReapLimits{MaxGasEstimated: utils.Some(int64(100_000))}, false)
	require.Equal(t, types.Txs{[]byte(a0), []byte(c0), []byte(smallCosmos)}, reaped)
}

func TestTxMempool_UpdateKeepsTxsRefusedForBlockGas(t *testing.T) {
	ctx := t.Context()
	sender := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	for _, tc := range []struct {
		name   string
		result *abci.ExecTxResult
		kept   bool
	}{
		{
			name:   "refused for block gas",
			result: &abci.ExecTxResult{Codespace: abci.CodespaceEVM, Code: abci.CodeBlockGasLimitReached},
			kept:   true,
		},
		{
			name:   "failed with the same code in another codespace",
			result: &abci.ExecTxResult{Code: abci.CodeBlockGasLimitReached},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newEVMNonceApp()
			app.setNonce(sender, 7)
			txmp := setup(TestConfig(), proxy.New(app), NopTxConstraintsFetcher)
			tx := types.Tx(fmt.Sprintf("evm=%s=%d=%d", sender.Hex(), 7, 10))
			_, err := txmp.CheckTx(ctx, tx)
			require.NoError(t, err)

			txmp.Lock()
			require.NoError(t, txmp.Update(ctx, 1, types.Txs{tx}, []*abci.ExecTxResult{tc.result},
				utils.OrPanic1(txmp.txConstraintsFetcher()), true))
			txmp.Unlock()

			if !tc.kept {
				require.Equal(t, 0, txmp.Size())
				return
			}
			reaped, _ := txmp.ReapTxs(ReapLimits{}, false)
			require.Equal(t, types.Txs{tx}, reaped, "the refused tx is reaped again for the next block")
		})
	}
}

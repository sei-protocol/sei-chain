package evmonly

import (
	"math/big"
	"runtime"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// frozenStateReader serves a MemoryState's accounts without its lock, as a read-only snapshot does.
type frozenStateReader struct {
	accounts map[common.Address]*StateAccount
}

func (r frozenStateReader) GetBalance(addr common.Address) *big.Int {
	if account, ok := r.accounts[addr]; ok && account.Balance != nil {
		return new(big.Int).Set(account.Balance)
	}
	return new(big.Int)
}

func (r frozenStateReader) GetNonce(addr common.Address) uint64 {
	if account, ok := r.accounts[addr]; ok {
		return account.Nonce
	}
	return 0
}

func (r frozenStateReader) GetCode(addr common.Address) []byte {
	if account, ok := r.accounts[addr]; ok {
		return account.Code
	}
	return nil
}

func (r frozenStateReader) GetState(addr common.Address, key common.Hash) common.Hash {
	if account, ok := r.accounts[addr]; ok {
		return account.Storage[key]
	}
	return common.Hash{}
}

func (r frozenStateReader) ReadAccount(addr common.Address) (baseAccount, bool) {
	return baseAccount{Balance: r.GetBalance(addr), Nonce: r.GetNonce(addr), Code: r.GetCode(addr)}, true
}

// BenchmarkOCCValidateAndMerge times validation and merge of one block's speculative results, which
// it computes once. spaced_conflicts places a conflict just past every parallel pass's minimum run.
func BenchmarkOCCValidateAndMerge(b *testing.B) {
	workers := runtime.GOMAXPROCS(0)
	for _, profile := range []occScenarioProfile{
		{name: "conflict_free_1800", blockTxs: 1800, independentPercent: 100},
		{name: "sparse_1800", blockTxs: 1800, independentPercent: 99},
		{name: "dense_320", blockTxs: 320},
		{name: "spaced_conflicts_20000", blockTxs: 20_000, conflictEvery: occMinParallelValidation + 1},
	} {
		b.Run(profile.name, func(b *testing.B) {
			ctx := b.Context()
			scenario := newOCCScenario(b, profile, 1)
			genesis := scenario.genesis()
			source := frozenStateReader{accounts: genesis.accounts}
			executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: workers, RejectUnappliableTxs: true}, withTestState(genesis))
			b.Cleanup(executor.Close)
			prepared, err := executor.PrepareBlock(ctx, scenario.block(b, 1))
			require.NoError(b, err)
			runner := newOCCSpeculativeRunner(executor, prepared)
			speculative := make([]occTxExecution, len(prepared.Txs))
			ranges := occRanges(len(prepared.Txs), occChunkSize(len(prepared.Txs), workers))
			require.NoError(b, runner.runRanges(ctx, executor.occPool, ranges, source, runner.blockGasLimit, speculative))
			results := make([]occTxExecution, len(speculative))

			b.ReportAllocs()
			for b.Loop() {
				copy(results, speculative)
				validated, finalState, _, err := executor.validateBlockSTM(ctx, runner, executor.occPool, source, results)
				if err != nil {
					b.Fatal(err)
				}
				blockResult, err := executor.mergeOCCResults(ctx, validated, finalState)
				if err != nil {
					b.Fatal(err)
				}
				blockResult.Release()
			}
		})
	}
}

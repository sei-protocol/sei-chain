package occ

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/server/config"
	"github.com/sei-protocol/sei-chain/occ_tests/messages"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
)

type benchConfig struct {
	name       string
	workers    int
	occEnabled bool
}

var occConfigs = []benchConfig{
	{name: "occ", workers: config.DefaultConcurrencyWorkers, occEnabled: true},
	{name: "sequential", workers: 1, occEnabled: false},
}

func runEVMBenchmark(b *testing.B, txCount int, cfg benchConfig, genTxs func(tCtx *utils.TestContext) []*utils.TestMessage) {
	b.Helper()
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)

	var totalGasUsed int64
	var totalTimedDuration time.Duration

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		iterCtx := utils.NewTestContext(b, accts, blockTime, cfg.workers, cfg.occEnabled)
		txMsgs := genTxs(iterCtx)
		txs := utils.ToTxBytes(iterCtx, txMsgs)
		b.StartTimer()

		startTime := time.Now()
		_, txResults, _, err := utils.ProcessBlockDirect(iterCtx, txs, cfg.occEnabled)
		timedDuration := time.Since(startTime)
		totalTimedDuration += timedDuration

		if err != nil {
			b.Fatalf("ProcessBlock error: %v", err)
		}
		if len(txResults) != len(txs) {
			b.Fatalf("expected %d tx results, got %d", len(txs), len(txResults))
		}

		for _, result := range txResults {
			totalGasUsed += result.GasUsed
		}
	}

	b.ReportMetric(float64(txCount), "txns/op")
	avgGasPerOp := float64(totalGasUsed) / float64(b.N)
	b.ReportMetric(avgGasPerOp, "gas/op")
	avgTimeSeconds := totalTimedDuration.Seconds() / float64(b.N)
	if avgTimeSeconds > 0 {
		b.ReportMetric(avgGasPerOp/avgTimeSeconds, "gas/sec")
		b.ReportMetric(float64(txCount)/avgTimeSeconds, "tps")
		b.ReportMetric(avgTimeSeconds/float64(txCount)*1e9, "ns/tx")
	}
}

func BenchmarkEVMTransfer(b *testing.B) {
	txCounts := []int{100, 500, 1000, 5000}
	for _, tc := range txCounts {
		for _, cfg := range occConfigs {
			tc := tc
			cfg := cfg
			b.Run(benchName(tc, cfg.name), func(b *testing.B) {
				runEVMBenchmark(b, tc, cfg, func(tCtx *utils.TestContext) []*utils.TestMessage {
					return utils.JoinMsgs(messages.EVMTransferNonConflicting(tCtx, tc))
				})
			})
		}
	}
}

func BenchmarkEVMTransferConflicting(b *testing.B) {
	txCounts := []int{100, 1000}
	for _, tc := range txCounts {
		for _, cfg := range occConfigs {
			tc := tc
			cfg := cfg
			b.Run(benchName(tc, cfg.name), func(b *testing.B) {
				runEVMBenchmark(b, tc, cfg, func(tCtx *utils.TestContext) []*utils.TestMessage {
					return utils.JoinMsgs(messages.EVMTransferConflicting(tCtx, tc))
				})
			})
		}
	}
}

func BenchmarkEVMTransferMixed(b *testing.B) {
	txCounts := []int{1000}
	for _, tc := range txCounts {
		for _, cfg := range occConfigs {
			tc := tc
			cfg := cfg
			half := tc / 2
			b.Run(benchName(tc, cfg.name), func(b *testing.B) {
				runEVMBenchmark(b, tc, cfg, func(tCtx *utils.TestContext) []*utils.TestMessage {
					conflicting := messages.EVMTransferConflicting(tCtx, half)
					nonConflicting := messages.EVMTransferNonConflicting(tCtx, tc-half)
					return utils.Shuffle(utils.JoinMsgs(conflicting, nonConflicting))
				})
			})
		}
	}
}

func benchName(txCount int, mode string) string {
	return "txs_" + itoa(txCount) + "/" + mode
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

package occ

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/occ_tests/messages"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	"github.com/sei-protocol/sei-load/config"
	"github.com/sei-protocol/sei-load/generator"
	"github.com/stretchr/testify/require"
)

// TestPerfEvmTransferNonConflictingWithTracing runs the same test as TestPerfEvmTransferNonConflicting
// but with OpenTelemetry tracing enabled. To use this:
//
//  1. Start Jaeger locally:
//     docker run -d --name jaeger \
//     -e COLLECTOR_ZIPKIN_HOST_PORT=:9411 \
//     -p 5775:5775/udp \
//     -p 6831:6831/udp \
//     -p 6832:6832/udp \
//     -p 5778:5778 \
//     -p 16686:16686 \
//     -p 14250:14250 \
//     -p 14268:14268 \
//     -p 14269:14269 \
//     -p 9411:9411 \
//     jaegertracing/all-in-one:latest
//
//  2. Run the test:
//     go test -v -run TestPerfEvmTransferNonConflictingWithTracing ./occ_tests
//
//  3. View traces in Jaeger UI:
//     Open http://localhost:16686 in your browser
//     Select "component-main" from the Service dropdown
//     Click "Find Traces"
//
// You'll see detailed traces showing:
// - RunTx spans with transaction execution
// - AnteHandler spans showing ante decorator execution
// - RunMsgs spans showing message handler execution
// - Individual decorator timings from console logs
func TestPerfEvmTransferNonConflictingWithTracing(t *testing.T) {
	g, err := generator.NewConfigBasedGenerator(&config.LoadConfig{
		ChainID:    713714, // Must match config.DefaultChainID
		SeiChainID: "test",
		Accounts: &config.AccountConfig{
			Accounts: 100,
		},
		Scenarios: []config.Scenario{
			{
				Name:   "EVMTransferNoop",
				Weight: 1,
			},
		},
	})

	require.NoError(t, err)
	runPerfTestWithTracing(t, Test{
		runs:  1,
		accts: 500,
		gen:   g,
		name:  "Test evm transfers non-conflicting with tracing",
		txs: func(tCtx *utils.TestContext) []*utils.TestMessage {
			return utils.JoinMsgs(
				messages.EVMGenerator(tCtx, g, 100),
			)
		},
	})
}

func runPerfTestWithTracing(t *testing.T, tt Test) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(tt.accts)
	ctx := utils.NewTestContextWithTracing(t, accts, blockTime, 500, true)
	_ = runBlock(t, tt, ctx, true)
	for range tt.runs {
		ctx.Ctx = ctx.Ctx.WithTraceSpanContext(context.Background())
		duration := runBlock(t, tt, ctx, true)
		fmt.Printf("duration = %v\n", duration)
		t.Logf("Traces available at http://localhost:16686")
	}
}

func runBlock(t *testing.T, tt Test, ctx *utils.TestContext, tracing bool) time.Duration {
	ctx.Ctx = ctx.Ctx.WithIsTracing(tracing)
	txs := tt.txs(ctx)
	_, pResults, _, duration, pErr := utils.RunWithOCC(ctx, txs)
	require.NoError(t, pErr, tt.name)
	require.Len(t, pResults, len(txs))
	assertTxResultCode(t, pResults, 0, tt.name)
	return duration
}

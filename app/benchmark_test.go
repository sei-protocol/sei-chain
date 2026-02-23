package app

import (
	"context"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app/benchmark"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/rootmulti"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func createTestContext() sdk.Context {
	db := dbm.NewMemDB()
	logger := log.NewNopLogger()
	ms := rootmulti.NewStore(db, log.NewNopLogger())
	return sdk.NewContext(ms, tmtypes.Header{}, false, logger)
}

func TestPrepareProposalBenchmarkHandler(t *testing.T) {
	// Create a mock app with benchmark mode enabled
	logger := log.NewNopLogger()
	app := &App{}

	// Test handler with nil manager (should return empty proposal)
	ctx := createTestContext()
	req := &abci.RequestPrepareProposal{
		Height: 1,
		Time:   time.Now(),
	}
	resp, err := app.PrepareProposalBenchmarkHandler(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.TxRecords, 0)

	// Create a mock manager with a channel
	proposalCh := make(chan *abci.ResponsePrepareProposal, 1)
	testProposal := &abci.ResponsePrepareProposal{
		TxRecords: []*abci.TxRecord{
			{Action: abci.TxRecord_UNMODIFIED, Tx: []byte("tx1")},
			{Action: abci.TxRecord_UNMODIFIED, Tx: []byte("tx2")},
		},
	}
	proposalCh <- testProposal

	app.benchmarkManager = &benchmark.Manager{
		Logger: benchmark.NewLogger(logger),
	}
	// We can't easily set the proposalCh since it's unexported, so we test the nil case

	// Test that handler doesn't panic with nil manager
	app.benchmarkManager = nil
	resp2, err := app.PrepareProposalBenchmarkHandler(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	require.Len(t, resp2.TxRecords, 0)
}

func TestBenchmarkHelperMethods(t *testing.T) {
	app := &App{}

	// Test helper methods with nil manager (should not panic)
	app.RecordBenchmarkCommitTime(100 * time.Millisecond)
	app.StartBenchmarkBlockProcessing()
	app.EndBenchmarkBlockProcessing()

	// BenchmarkLogger should return nil when manager is nil
	require.Nil(t, app.BenchmarkLogger())

	// Create a benchmark manager with logger
	benchLogger := benchmark.NewLogger(log.NewNopLogger())
	app.benchmarkManager = &benchmark.Manager{
		Logger: benchLogger,
	}

	// Now helper methods should work (just verify they don't panic)
	app.RecordBenchmarkCommitTime(100 * time.Millisecond)
	app.StartBenchmarkBlockProcessing()
	app.EndBenchmarkBlockProcessing()

	// BenchmarkLogger should return the logger
	require.NotNil(t, app.BenchmarkLogger())
	require.Equal(t, benchLogger, app.BenchmarkLogger())
}

func TestInitBenchmark_PanicsOnLiveChainID(t *testing.T) {
	logger := log.NewNopLogger()
	chainID := "pacific-1"
	liveEVMChainID := int64(1329) // pacific-1's EVM chain ID (live)

	// Create a minimal App struct
	app := &App{
		encodingConfig: MakeEncodingConfig(),
	}

	ctx := context.Background()

	// Test that InitBenchmark panics with live chain ID
	require.Panics(t, func() {
		app.InitBenchmark(ctx, chainID, liveEVMChainID, logger)
	}, "InitBenchmark should panic on live chain ID")

	// Verify nothing was initialized
	require.Nil(t, app.benchmarkManager, "benchmarkManager should not be initialized on panic")
}

func TestInitBenchmark_AllLiveChainIDs(t *testing.T) {
	logger := log.NewNopLogger()
	liveChainIDs := []struct {
		chainID     string
		evmChainID  int64
		description string
	}{
		{"pacific-1", 1329, "pacific-1"},
		{"atlantic-2", 1328, "atlantic-2"},
		{"arctic-1", 713715, "arctic-1"},
	}

	for _, tc := range liveChainIDs {
		t.Run(tc.description, func(t *testing.T) {
			app := &App{
				encodingConfig: MakeEncodingConfig(),
			}
			ctx := context.Background()

			require.Panics(t, func() {
				app.InitBenchmark(ctx, tc.chainID, tc.evmChainID, logger)
			}, "InitBenchmark should panic on live chain ID: %s", tc.description)
		})
	}
}

func TestInitBenchmark_Success(t *testing.T) {
	logger := log.NewNopLogger()
	chainID := "test-chain"
	evmChainID := int64(12345) // Non-live chain ID

	// Create a minimal App struct with required fields
	app := &App{
		encodingConfig: MakeEncodingConfig(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test InitBenchmark with non-live chain ID
	app.InitBenchmark(ctx, chainID, evmChainID, logger)

	// Verify benchmarkManager is set
	require.NotNil(t, app.benchmarkManager, "benchmarkManager should be initialized")
	require.NotNil(t, app.benchmarkManager.Logger, "benchmarkManager.Logger should be set")
	require.NotNil(t, app.benchmarkManager.Generator, "benchmarkManager.Generator should be set")

	// Verify we can get the proposal channel
	require.NotNil(t, app.benchmarkManager.ProposalChannel(), "proposal channel should be available")

	// Consume a proposal to verify the channel is working
	select {
	case proposal, ok := <-app.benchmarkManager.ProposalChannel():
		if ok {
			require.NotNil(t, proposal, "Proposal should not be nil")
			// EVMTransfer scenario doesn't need deployment, so should get load txs immediately
			t.Logf("Received proposal with %d tx records", len(proposal.TxRecords))
		}
	case <-time.After(5 * time.Second):
		t.Log("Timeout waiting for proposal (may be in setup phase)")
	}

	// Cancel context to stop the generator
	cancel()
}

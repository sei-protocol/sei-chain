package app

import (
	"context"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/store/rootmulti"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"
)

func TestCalculateTPS(t *testing.T) {
	tests := []struct {
		name     string
		txCount  int64
		duration time.Duration
		expected float64
	}{
		{
			name:     "normal case - 1000 txs in 1 second",
			txCount:  1000,
			duration: 1 * time.Second,
			expected: 1000.0,
		},
		{
			name:     "normal case - 5000 txs in 5 seconds",
			txCount:  5000,
			duration: 5 * time.Second,
			expected: 1000.0,
		},
		{
			name:     "zero duration",
			txCount:  1000,
			duration: 0,
			expected: 0.0,
		},
		{
			name:     "negative duration",
			txCount:  1000,
			duration: -1 * time.Second,
			expected: 0.0,
		},
		{
			name:     "zero transactions",
			txCount:  0,
			duration: 5 * time.Second,
			expected: 0.0,
		},
		{
			name:     "fractional seconds",
			txCount:  1000,
			duration: 2 * time.Second,
			expected: 500.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateTPS(tt.txCount, tt.duration)
			require.InDelta(t, tt.expected, result, 0.01)
		})
	}
}

func TestCalculateAvgBlockTime(t *testing.T) {
	tests := []struct {
		name           string
		totalBlockTime time.Duration
		blockTimeCount int64
		expected       int64
	}{
		{
			name:           "normal case - 5 blocks with 1000ms total",
			totalBlockTime: 5000 * time.Millisecond,
			blockTimeCount: 5,
			expected:       1000,
		},
		{
			name:           "normal case - 10 blocks with 2000ms total",
			totalBlockTime: 20000 * time.Millisecond,
			blockTimeCount: 10,
			expected:       2000,
		},
		{
			name:           "zero count",
			totalBlockTime: 5000 * time.Millisecond,
			blockTimeCount: 0,
			expected:       0,
		},
		{
			name:           "zero total time",
			totalBlockTime: 0,
			blockTimeCount: 5,
			expected:       0,
		},
		{
			name:           "fractional milliseconds",
			totalBlockTime: 3333 * time.Millisecond,
			blockTimeCount: 3,
			expected:       1111, // 3333/3 = 1111ms
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateAvgBlockTime(tt.totalBlockTime, tt.blockTimeCount)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestBenchmarkLogger_Increment(t *testing.T) {
	logger := log.NewNopLogger()
	bl := &benchmarkLogger{
		logger: logger,
	}

	baseTime := time.Now()

	// First increment should initialize lastFlushTime
	bl.Increment(100, baseTime, 1)
	require.False(t, bl.lastFlushTime.IsZero())
	require.Equal(t, int64(100), bl.txCount)
	require.Equal(t, int64(1), bl.blockCount)
	require.Equal(t, int64(1), bl.latestHeight)

	// Second increment should update maxBlockTime if larger
	time2 := baseTime.Add(2 * time.Second)
	bl.Increment(200, time2, 2)
	require.Equal(t, int64(300), bl.txCount)
	require.Equal(t, int64(2), bl.blockCount)
	require.Equal(t, int64(2), bl.latestHeight)
	require.Equal(t, 2*time.Second, bl.maxBlockTime)
	require.Equal(t, int64(1), bl.blockTimeCount)

	// Third increment with smaller time diff should not update maxBlockTime
	time3 := time2.Add(500 * time.Millisecond)
	bl.Increment(150, time3, 3)
	require.Equal(t, int64(450), bl.txCount)
	require.Equal(t, int64(3), bl.blockCount)
	require.Equal(t, int64(3), bl.latestHeight)
	require.Equal(t, 2*time.Second, bl.maxBlockTime) // Still the max
	require.Equal(t, int64(2), bl.blockTimeCount)

	// Increment with higher height should update latestHeight
	bl.Increment(100, time3, 5)
	require.Equal(t, int64(5), bl.latestHeight)
}

func TestBenchmarkLogger_GetAndResetStats(t *testing.T) {
	logger := log.NewNopLogger()
	bl := &benchmarkLogger{
		logger: logger,
	}

	baseTime := time.Now()
	bl.lastFlushTime = baseTime

	// Set up some stats
	bl.txCount = 1000
	bl.blockCount = 10
	bl.latestHeight = 100
	bl.maxBlockTime = 2 * time.Second
	bl.totalBlockTime = 10 * time.Second
	bl.blockTimeCount = 9 // 9 intervals between 10 blocks

	// Wait a bit to ensure duration > 0
	time.Sleep(10 * time.Millisecond)
	now := time.Now()

	stats, prevTime := bl.getAndResetStats(now)

	// Check stats were captured correctly
	require.Equal(t, int64(1000), stats.txCount)
	require.Equal(t, int64(10), stats.blockCount)
	require.Equal(t, int64(100), stats.latestHeight)
	require.Equal(t, int64(2000), stats.maxBlockTimeMs)
	require.InDelta(t, 1111, stats.avgBlockTimeMs, 1) // 10000ms / 9 ≈ 1111ms
	require.Equal(t, baseTime, prevTime)

	// Check TPS calculation
	duration := now.Sub(baseTime)
	expectedTPS := calculateTPS(1000, duration)
	require.InDelta(t, expectedTPS, stats.tps, 0.01)

	// Check counters were reset
	require.Equal(t, int64(0), bl.txCount)
	require.Equal(t, int64(0), bl.blockCount)
	require.Equal(t, int64(0), bl.latestHeight)
	require.Equal(t, time.Duration(0), bl.maxBlockTime)
	require.Equal(t, time.Duration(0), bl.totalBlockTime)
	require.Equal(t, int64(0), bl.blockTimeCount)
	require.Equal(t, now, bl.lastFlushTime)
}

func TestBenchmarkLogger_StartStop(t *testing.T) {
	logger := log.NewNopLogger()
	bl := &benchmarkLogger{
		logger: logger,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the logger
	done := make(chan bool)
	go func() {
		bl.Start(ctx)
		done <- true
	}()

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	// Add some increments
	baseTime := time.Now()
	bl.Increment(100, baseTime, 1)
	bl.Increment(200, baseTime.Add(time.Second), 2)

	// Wait for at least one flush (should happen after 5 seconds, but we'll cancel earlier)
	time.Sleep(100 * time.Millisecond)

	// Cancel context to stop the logger
	cancel()

	// Wait for goroutine to finish
	select {
	case <-done:
		// Successfully stopped
	case <-time.After(1 * time.Second):
		t.Fatal("Logger did not stop within timeout")
	}
}

func TestBenchmarkLogger_FirstFlushZeroTPS(t *testing.T) {
	logger := log.NewNopLogger()
	bl := &benchmarkLogger{
		logger: logger,
	}

	baseTime := time.Now()

	// First increment initializes lastFlushTime
	bl.Increment(100, baseTime, 1)

	// Immediately flush (should have zero TPS since duration is near zero or zero)
	now := baseTime.Add(1 * time.Nanosecond) // Very small duration
	stats, _ := bl.getAndResetStats(now)

	// TPS should be 0 for first flush (duration too small or zero)
	require.Equal(t, float64(0), stats.tps)
	require.Equal(t, int64(100), stats.txCount)
}

func createTestContext() sdk.Context {
	db := dbm.NewMemDB()
	logger := log.NewNopLogger()
	ms := rootmulti.NewStore(db, log.NewNopLogger(), []string{})
	return sdk.NewContext(ms, tmtypes.Header{}, false, logger)
}

func TestPrepareProposalGeneratorHandler(t *testing.T) {
	// Create a mock app with benchmark mode enabled
	logger := log.NewNopLogger()
	app := &App{
		benchmarkLogger: &benchmarkLogger{
			logger: logger,
		},
	}

	// Create a test channel with a proposal
	proposalCh := make(chan *abci.ResponsePrepareProposal, 1)
	testProposal := &abci.ResponsePrepareProposal{
		TxRecords: []*abci.TxRecord{
			{Action: abci.TxRecord_UNMODIFIED, Tx: []byte("tx1")},
			{Action: abci.TxRecord_UNMODIFIED, Tx: []byte("tx2")},
		},
	}
	proposalCh <- testProposal
	app.benchmarkProposalCh = proposalCh

	ctx := createTestContext()

	// Test handler with available proposal
	req := &abci.RequestPrepareProposal{
		Height: 1,
		Time:   time.Now(),
	}
	resp, err := app.PrepareProposalGeneratorHandler(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.TxRecords, 2)
	require.Equal(t, int64(2), app.benchmarkLogger.txCount)
	require.Equal(t, int64(1), app.benchmarkLogger.blockCount)

	// Test handler with no proposal available (default case)
	emptyCh := make(chan *abci.ResponsePrepareProposal, 1)
	app.benchmarkProposalCh = emptyCh
	resp2, err := app.PrepareProposalGeneratorHandler(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	require.Len(t, resp2.TxRecords, 0)

	// Test handler with closed channel
	closedCh := make(chan *abci.ResponsePrepareProposal)
	close(closedCh)
	app.benchmarkProposalCh = closedCh
	resp3, err := app.PrepareProposalGeneratorHandler(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp3)
	require.Len(t, resp3.TxRecords, 0)
}

func TestBenchmarkLogger_ConcurrentIncrement(t *testing.T) {
	logger := log.NewNopLogger()
	bl := &benchmarkLogger{
		logger: logger,
	}

	baseTime := time.Now()
	numGoroutines := 10
	iterationsPerGoroutine := 100

	// Run concurrent increments
	done := make(chan bool, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < iterationsPerGoroutine; j++ {
				bl.Increment(1, baseTime.Add(time.Duration(id*iterationsPerGoroutine+j)*time.Millisecond), int64(id*iterationsPerGoroutine+j))
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify final counts
	require.Equal(t, int64(numGoroutines*iterationsPerGoroutine), bl.txCount)
	require.Equal(t, int64(numGoroutines*iterationsPerGoroutine), bl.blockCount)
}

func TestBenchmarkLogger_BlockTimeCalculations(t *testing.T) {
	logger := log.NewNopLogger()
	bl := &benchmarkLogger{
		logger: logger,
	}

	baseTime := time.Now()

	// Increment with increasing block times
	bl.Increment(100, baseTime, 1)
	bl.Increment(100, baseTime.Add(100*time.Millisecond), 2) // 100ms diff
	bl.Increment(100, baseTime.Add(250*time.Millisecond), 3) // 150ms diff
	bl.Increment(100, baseTime.Add(600*time.Millisecond), 4) // 350ms diff (max)

	require.Equal(t, 350*time.Millisecond, bl.maxBlockTime)
	require.Equal(t, int64(3), bl.blockTimeCount)
	require.Equal(t, 600*time.Millisecond, bl.totalBlockTime) // 100 + 150 + 350

	// Flush and verify stats
	bl.lastFlushTime = baseTime
	time.Sleep(10 * time.Millisecond)
	stats, _ := bl.getAndResetStats(time.Now())
	require.Equal(t, int64(350), stats.maxBlockTimeMs)
	require.Equal(t, int64(200), stats.avgBlockTimeMs) // 600ms / 3 = 200ms
}

func TestNewGeneratorCh(t *testing.T) {
	logger := log.NewNopLogger()
	txConfig := MakeEncodingConfig().TxConfig
	chainID := "test-chain"
	evmChainID := int64(12345) // Non-live chain ID

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create the generator channel
	ch := NewGeneratorCh(ctx, txConfig, chainID, evmChainID, logger)

	// Consume a few proposals from the channel
	proposalsReceived := 0
	maxProposals := 3
	timeout := 10 * time.Second

	for proposalsReceived < maxProposals {
		select {
		case proposal, ok := <-ch:
			if !ok {
				// Channel closed
				t.Log("Channel closed")
				return
			}
			require.NotNil(t, proposal, "Proposal should not be nil")
			require.NotNil(t, proposal.TxRecords, "TxRecords should not be nil")
			require.Greater(t, len(proposal.TxRecords), 0, "Should have at least one tx record")

			// Verify tx records are valid
			for _, txRecord := range proposal.TxRecords {
				require.NotNil(t, txRecord, "TxRecord should not be nil")
				require.NotEmpty(t, txRecord.Tx, "Tx should not be empty")
				require.Equal(t, abci.TxRecord_UNMODIFIED, txRecord.Action)
			}

			proposalsReceived++
			t.Logf("Received proposal %d with %d tx records", proposalsReceived, len(proposal.TxRecords))

		case <-time.After(timeout):
			t.Fatalf("Timeout waiting for proposal %d after %v", proposalsReceived+1, timeout)
		}
	}

	// Cancel context to stop the generator
	cancel()

	// Wait a bit for the goroutine to exit
	time.Sleep(100 * time.Millisecond)

	// Verify channel is eventually closed (or will be closed)
	// Try to read one more time - should either get nil/closed or timeout quickly
	select {
	case proposal, ok := <-ch:
		if ok {
			// Got one more proposal before close
			require.NotNil(t, proposal)
		} else {
			// Channel closed as expected
			t.Log("Channel closed after context cancellation")
		}
	case <-time.After(1 * time.Second):
		// Channel might still be open but generator should have stopped
		t.Log("Channel still open after cancellation (may close later)")
	}

	require.GreaterOrEqual(t, proposalsReceived, maxProposals, "Should have received at least the expected number of proposals")
}

func TestFlushLog(t *testing.T) {
	// Create a logger that captures output
	logger := log.NewNopLogger()
	bl := &benchmarkLogger{
		logger: logger,
	}

	baseTime := time.Now()
	bl.lastFlushTime = baseTime

	// Set up some stats
	bl.txCount = 5000
	bl.blockCount = 10
	bl.latestHeight = 100
	bl.maxBlockTime = 2 * time.Second
	bl.totalBlockTime = 10 * time.Second
	bl.blockTimeCount = 9

	// Wait a bit to ensure duration > 0
	time.Sleep(10 * time.Millisecond)

	// Call FlushLog - this should not panic and should reset stats
	bl.FlushLog()

	// Verify stats were reset after flush
	require.Equal(t, int64(0), bl.txCount)
	require.Equal(t, int64(0), bl.blockCount)
	require.Equal(t, int64(0), bl.latestHeight)
	require.Equal(t, time.Duration(0), bl.maxBlockTime)
	require.Equal(t, time.Duration(0), bl.totalBlockTime)
	require.Equal(t, int64(0), bl.blockTimeCount)
	require.False(t, bl.lastFlushTime.IsZero(), "lastFlushTime should be updated")

	// Test FlushLog with zero stats (should not panic)
	bl.FlushLog()
	require.Equal(t, int64(0), bl.txCount)

	// Test FlushLog with some TPS
	bl.txCount = 1000
	bl.blockCount = 5
	bl.lastFlushTime = time.Now().Add(-2 * time.Second)
	time.Sleep(10 * time.Millisecond)
	bl.FlushLog()

	// Stats should be reset after flush
	require.Equal(t, int64(0), bl.txCount)
}

func TestNewGeneratorCh_ContextCancellation(t *testing.T) {
	logger := log.NewNopLogger()
	txConfig := MakeEncodingConfig().TxConfig
	chainID := "test-chain"
	evmChainID := int64(12345)

	ctx, cancel := context.WithCancel(context.Background())

	// Create the generator channel
	ch := NewGeneratorCh(ctx, txConfig, chainID, evmChainID, logger)

	// Immediately cancel the context
	cancel()

	// Wait a bit for the goroutine to process cancellation
	time.Sleep(200 * time.Millisecond)

	// Channel should eventually close or stop producing
	// Try to read - should either get nothing or channel should be closed
	select {
	case proposal, ok := <-ch:
		if ok {
			// Got a proposal before cancellation took effect
			require.NotNil(t, proposal)
		} else {
			// Channel closed as expected
			t.Log("Channel closed after context cancellation")
		}
	case <-time.After(2 * time.Second):
		// Channel might still be open but should stop producing soon
		t.Log("Channel still open after cancellation")
	}
}

func TestInitGenerator(t *testing.T) {
	logger := log.NewNopLogger()
	chainID := "test-chain"
	evmChainID := int64(12345) // Non-live chain ID

	// Create a minimal App struct with required fields
	app := &App{
		encodingConfig: MakeEncodingConfig(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test InitGenerator with non-live chain ID
	app.InitGenerator(ctx, chainID, evmChainID, logger)

	// Verify benchmarkLogger is set
	require.NotNil(t, app.benchmarkLogger, "benchmarkLogger should be initialized")
	require.NotNil(t, app.benchmarkLogger.logger, "benchmarkLogger.logger should be set")

	// Verify benchmarkProposalCh is set
	require.NotNil(t, app.benchmarkProposalCh, "benchmarkProposalCh should be initialized")

	// Consume a few proposals to verify the channel is working
	proposalsReceived := 0
	maxProposals := 2
	timeout := 5 * time.Second

	for proposalsReceived < maxProposals {
		select {
		case proposal, ok := <-app.benchmarkProposalCh:
			if !ok {
				t.Fatal("Channel closed unexpectedly")
			}
			require.NotNil(t, proposal, "Proposal should not be nil")
			require.Greater(t, len(proposal.TxRecords), 0, "Should have tx records")
			proposalsReceived++
			t.Logf("Received proposal %d with %d tx records", proposalsReceived, len(proposal.TxRecords))
		case <-time.After(timeout):
			t.Fatalf("Timeout waiting for proposal %d", proposalsReceived+1)
		}
	}

	// Test that Increment works on the benchmarkLogger
	baseTime := time.Now()
	app.benchmarkLogger.Increment(100, baseTime, 1)
	require.Equal(t, int64(100), app.benchmarkLogger.txCount)
	require.Equal(t, int64(1), app.benchmarkLogger.blockCount)

	// Cancel context to stop the generator
	cancel()

	// Wait for goroutines to clean up
	time.Sleep(200 * time.Millisecond)

	// Verify channel eventually closes or stops producing
	select {
	case proposal, ok := <-app.benchmarkProposalCh:
		if ok {
			// Got one more proposal before close
			require.NotNil(t, proposal)
		} else {
			// Channel closed as expected
			t.Log("Channel closed after context cancellation")
		}
	case <-time.After(2 * time.Second):
		// Channel might still be open but generator should have stopped
		t.Log("Channel still open after cancellation")
	}

	require.GreaterOrEqual(t, proposalsReceived, maxProposals, "Should have received at least the expected number of proposals")
}

func TestInitGenerator_PanicsOnLiveChainID(t *testing.T) {
	logger := log.NewNopLogger()
	chainID := "pacific-1"
	liveEVMChainID := int64(1329) // pacific-1's EVM chain ID (live)

	// Create a minimal App struct
	app := &App{
		encodingConfig: MakeEncodingConfig(),
	}

	ctx := context.Background()

	// Test that InitGenerator panics with live chain ID
	require.Panics(t, func() {
		app.InitGenerator(ctx, chainID, liveEVMChainID, logger)
	}, "InitGenerator should panic on live chain ID")

	// Verify nothing was initialized
	require.Nil(t, app.benchmarkLogger, "benchmarkLogger should not be initialized on panic")
	require.Nil(t, app.benchmarkProposalCh, "benchmarkProposalCh should not be initialized on panic")
}

func TestInitGenerator_AllLiveChainIDs(t *testing.T) {
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
				app.InitGenerator(ctx, tc.chainID, tc.evmChainID, logger)
			}, "InitGenerator should panic on live chain ID: %s", tc.description)
		})
	}
}

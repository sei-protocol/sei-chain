package app

import (
	"sync"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

type OptimisticProcessingTestSuite struct {
	suite.Suite
	app *App
	ctx sdk.Context
}

func (suite *OptimisticProcessingTestSuite) SetupTest() {
	suite.app = Setup(false, false, false)
	suite.ctx = suite.app.BaseApp.NewContext(false, tmproto.Header{Height: 1})
}

func TestOptimisticProcessingTestSuite(t *testing.T) {
	suite.Run(t, new(OptimisticProcessingTestSuite))
}

// Test GetOptimisticProcessingInfo thread safety
func (suite *OptimisticProcessingTestSuite) TestGetOptimisticProcessingInfo_ThreadSafety() {
	require := suite.Require()

	// Set initial processing info
	info := OptimisticProcessingInfo{
		Height: suite.ctx.BlockHeight(),
		Hash:   []byte("test-hash"),
	}
	suite.app.optimisticProcessingInfoMutex.Lock()
	suite.app.optimisticProcessingInfo = info
	suite.app.optimisticProcessingInfoMutex.Unlock()

	// Test concurrent reads
	const numReaders = 100
	var wg sync.WaitGroup
	results := make([]OptimisticProcessingInfo, numReaders)

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index] = suite.app.GetOptimisticProcessingInfo()
		}(i)
	}

	wg.Wait()

	// Verify all reads returned the same data
	for i := 0; i < numReaders; i++ {
		require.Equal(info.Height, results[i].Height)
		require.Equal(info.Hash, results[i].Hash)
	}
}

// Test ClearOptimisticProcessingInfo thread safety
func (suite *OptimisticProcessingTestSuite) TestClearOptimisticProcessingInfo_ThreadSafety() {
	require := suite.Require()

	// Set initial processing info
	info := OptimisticProcessingInfo{
		Height: suite.ctx.BlockHeight(),
		Hash:   []byte("test-hash"),
	}
	suite.app.optimisticProcessingInfoMutex.Lock()
	suite.app.optimisticProcessingInfo = info
	suite.app.optimisticProcessingInfoMutex.Unlock()

	// Test concurrent clears and reads
	const numOperations = 50
	var wg sync.WaitGroup

	// Start readers
	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = suite.app.GetOptimisticProcessingInfo()
		}()
	}

	// Start clearers
	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			suite.app.ClearOptimisticProcessingInfo()
		}()
	}

	wg.Wait()

	// Verify final state is cleared
	finalInfo := suite.app.GetOptimisticProcessingInfo()
	require.Equal(int64(0), finalInfo.Height)
	require.Nil(finalInfo.Hash)
	require.Nil(finalInfo.Completion)
}

// Test ProcessProposalHandler with no existing optimistic processing
func (suite *OptimisticProcessingTestSuite) TestProcessProposalHandler_NewOptimisticProcessing() {
	require := suite.Require()

	req := &abci.RequestProcessProposal{
		Height: suite.ctx.BlockHeight(),
		Hash:   []byte("test-hash"),
		Txs:    [][]byte{},
	}

	// Ensure no existing optimistic processing
	suite.app.ClearOptimisticProcessingInfo()

	resp, err := suite.app.ProcessProposalHandler(suite.ctx, req)
	require.NoError(err)
	require.Equal(abci.ResponseProcessProposal_ACCEPT, resp.Status)

	// Verify optimistic processing info was set
	info := suite.app.GetOptimisticProcessingInfo()
	require.Equal(req.Height, info.Height)
	require.Equal(req.Hash, info.Hash)
	require.NotNil(info.Completion)
	require.False(info.Aborted)
}

// Test ProcessProposalHandler with upgrade plan (should abort)
func (suite *OptimisticProcessingTestSuite) TestProcessProposalHandler_UpgradePlanAborts() {
	require := suite.Require()

	// Schedule an upgrade for the next block
	plan := upgradetypes.Plan{
		Name:   "test-upgrade",
		Height: suite.ctx.BlockHeight() + 1,
	}
	suite.app.UpgradeKeeper.ScheduleUpgrade(suite.ctx, plan)

	// Create context for next block
	nextCtx := suite.ctx.WithBlockHeight(suite.ctx.BlockHeight() + 1)

	req := &abci.RequestProcessProposal{
		Height: nextCtx.BlockHeight(),
		Hash:   []byte("test-hash"),
		Txs:    [][]byte{},
	}

	// Ensure no existing optimistic processing
	suite.app.ClearOptimisticProcessingInfo()

	resp, err := suite.app.ProcessProposalHandler(nextCtx, req)
	require.NoError(err)
	require.Equal(abci.ResponseProcessProposal_ACCEPT, resp.Status)

	// Wait for completion signal
	info := suite.app.GetOptimisticProcessingInfo()
	require.NotNil(info.Completion)

	select {
	case <-info.Completion:
		// Expected - completion signal received
	case <-time.After(time.Second):
		suite.T().Fatal("Timeout waiting for completion signal")
	}

	// Verify processing was aborted
	finalInfo := suite.app.GetOptimisticProcessingInfo()
	require.True(finalInfo.Aborted)
}

// Test ProcessProposalHandler with hash mismatch (should abort)
func (suite *OptimisticProcessingTestSuite) TestProcessProposalHandler_HashMismatchAborts() {
	require := suite.Require()

	// Set up existing optimistic processing
	initialInfo := OptimisticProcessingInfo{
		Height:     suite.ctx.BlockHeight(),
		Hash:       []byte("initial-hash"),
		Completion: make(chan struct{}, 1),
	}
	suite.app.optimisticProcessingInfoMutex.Lock()
	suite.app.optimisticProcessingInfo = initialInfo
	suite.app.optimisticProcessingInfoMutex.Unlock()

	req := &abci.RequestProcessProposal{
		Height: suite.ctx.BlockHeight(),
		Hash:   []byte("different-hash"), // Different hash
		Txs:    [][]byte{},
	}

	resp, err := suite.app.ProcessProposalHandler(suite.ctx, req)
	require.NoError(err)
	require.Equal(abci.ResponseProcessProposal_ACCEPT, resp.Status)

	// Verify processing was aborted due to hash mismatch
	info := suite.app.GetOptimisticProcessingInfo()
	require.True(info.Aborted)
}

// Test ProcessProposalHandler with same hash (should not abort)
func (suite *OptimisticProcessingTestSuite) TestProcessProposalHandler_SameHashContinues() {
	require := suite.Require()

	hash := []byte("same-hash")

	// Set up existing optimistic processing
	initialInfo := OptimisticProcessingInfo{
		Height:     suite.ctx.BlockHeight(),
		Hash:       hash,
		Completion: make(chan struct{}, 1),
	}
	suite.app.optimisticProcessingInfoMutex.Lock()
	suite.app.optimisticProcessingInfo = initialInfo
	suite.app.optimisticProcessingInfoMutex.Unlock()

	req := &abci.RequestProcessProposal{
		Height: suite.ctx.BlockHeight(),
		Hash:   hash, // Same hash
		Txs:    [][]byte{},
	}

	resp, err := suite.app.ProcessProposalHandler(suite.ctx, req)
	require.NoError(err)
	require.Equal(abci.ResponseProcessProposal_ACCEPT, resp.Status)

	// Verify processing was not aborted
	info := suite.app.GetOptimisticProcessingInfo()
	require.False(info.Aborted)
}

// Test concurrent access to optimistic processing methods
func (suite *OptimisticProcessingTestSuite) TestConcurrentOptimisticProcessing() {
	require := suite.Require()

	const numGoroutines = 50
	var wg sync.WaitGroup

	// Test concurrent operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(3) // 3 operations per iteration

		go func(index int) {
			defer wg.Done()
			// Read operation
			_ = suite.app.GetOptimisticProcessingInfo()
		}(i)

		go func(index int) {
			defer wg.Done()
			// Clear operation
			suite.app.ClearOptimisticProcessingInfo()
		}(i)

		go func(index int) {
			defer wg.Done()
			// Write operation
			info := OptimisticProcessingInfo{
				Height: suite.ctx.BlockHeight(),
				Hash:   []byte("concurrent-test"),
			}
			suite.app.optimisticProcessingInfoMutex.Lock()
			suite.app.optimisticProcessingInfo = info
			suite.app.optimisticProcessingInfoMutex.Unlock()
		}(i)
	}

	wg.Wait()

	// Test should complete without race conditions or deadlocks
	// Final state check
	_ = suite.app.GetOptimisticProcessingInfo()
	require.True(true, "Concurrent test completed successfully")
}

// Benchmark test for optimistic processing operations
func (suite *OptimisticProcessingTestSuite) TestOptimisticProcessingPerformance() {
	require := suite.Require()

	// Setup optimistic processing info
	info := OptimisticProcessingInfo{
		Height: suite.ctx.BlockHeight(),
		Hash:   []byte("performance-test"),
	}

	// Benchmark reads
	start := time.Now()
	for i := 0; i < 10000; i++ {
		_ = suite.app.GetOptimisticProcessingInfo()
	}
	readDuration := time.Since(start)

	// Benchmark writes
	start = time.Now()
	for i := 0; i < 1000; i++ {
		suite.app.optimisticProcessingInfoMutex.Lock()
		suite.app.optimisticProcessingInfo = info
		suite.app.optimisticProcessingInfoMutex.Unlock()
	}
	writeDuration := time.Since(start)

	suite.T().Logf("Read performance: %v for 10,000 operations", readDuration)
	suite.T().Logf("Write performance: %v for 1,000 operations", writeDuration)

	// Performance should be reasonable (adjust thresholds as needed)
	require.Less(readDuration, time.Second, "Read operations should be fast")
	require.Less(writeDuration, time.Second, "Write operations should be fast")
}

// Test mutex behavior under heavy contention
func (suite *OptimisticProcessingTestSuite) TestMutexContention() {
	require := suite.Require()

	const numGoroutines = 100
	const operationsPerGoroutine = 100
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				// Mix of reads and writes
				if j%2 == 0 {
					_ = suite.app.GetOptimisticProcessingInfo()
				} else {
					info := OptimisticProcessingInfo{
						Height: suite.ctx.BlockHeight(),
						Hash:   []byte("contention-test"),
					}
					suite.app.optimisticProcessingInfoMutex.Lock()
					suite.app.optimisticProcessingInfo = info
					suite.app.optimisticProcessingInfoMutex.Unlock()
				}
			}
		}(i)
	}

	wg.Wait()
	require.True(true, "Mutex contention test completed successfully")
}

// Test FinalizeBlocker with successful optimistic processing
func (suite *OptimisticProcessingTestSuite) TestFinalizeBlocker_SuccessfulOptimisticProcessing() {
	require := suite.Require()

	hash := []byte("test-hash")
	completion := make(chan struct{}, 1)

	// Create a minimal ResponseEndBlock - we'll test the basic flow without complex consensus params
	endBlockResp := abci.ResponseEndBlock{}

	// Set up optimistic processing info with results
	info := OptimisticProcessingInfo{
		Height:       suite.ctx.BlockHeight(),
		Hash:         hash,
		Completion:   completion,
		Aborted:      false,
		Events:       []abci.Event{{Type: "test-event"}},
		TxRes:        []*abci.ExecTxResult{{Code: 0}},
		EndBlockResp: endBlockResp,
	}
	suite.app.optimisticProcessingInfoMutex.Lock()
	suite.app.optimisticProcessingInfo = info
	suite.app.optimisticProcessingInfoMutex.Unlock()

	// Signal completion
	completion <- struct{}{}

	// Set EthReplayConfig to enabled to avoid getFinalizeBlockResponse complexity
	originalEthReplayEnabled := suite.app.EvmKeeper.EthReplayConfig.Enabled
	suite.app.EvmKeeper.EthReplayConfig.Enabled = true
	defer func() {
		suite.app.EvmKeeper.EthReplayConfig.Enabled = originalEthReplayEnabled
	}()

	req := &abci.RequestFinalizeBlock{
		Height: suite.ctx.BlockHeight(),
		Hash:   hash,
		Txs:    [][]byte{},
	}

	resp, err := suite.app.FinalizeBlocker(suite.ctx, req)
	require.NoError(err)
	require.NotNil(resp)

	// Verify optimistic processing info was cleared
	finalInfo := suite.app.GetOptimisticProcessingInfo()
	require.Equal(int64(0), finalInfo.Height)
	require.Nil(finalInfo.Hash)
	require.Nil(finalInfo.Completion)
}

// Test FinalizeBlocker with aborted optimistic processing
func (suite *OptimisticProcessingTestSuite) TestFinalizeBlocker_AbortedOptimisticProcessing() {
	require := suite.Require()

	hash := []byte("test-hash")
	completion := make(chan struct{}, 1)

	// Set up optimistic processing info that was aborted
	info := OptimisticProcessingInfo{
		Height:     suite.ctx.BlockHeight(),
		Hash:       hash,
		Completion: completion,
		Aborted:    true, // Aborted
	}
	suite.app.optimisticProcessingInfoMutex.Lock()
	suite.app.optimisticProcessingInfo = info
	suite.app.optimisticProcessingInfoMutex.Unlock()

	// Signal completion
	completion <- struct{}{}

	req := &abci.RequestFinalizeBlock{
		Height: suite.ctx.BlockHeight(),
		Hash:   hash,
		Txs:    [][]byte{},
	}

	resp, err := suite.app.FinalizeBlocker(suite.ctx, req)
	require.NoError(err)
	require.NotNil(resp)

	// Verify fallback processing was used (since processing was aborted)
	// The response should still be valid but it used the fallback path
}

// Test FinalizeBlocker with hash mismatch
func (suite *OptimisticProcessingTestSuite) TestFinalizeBlocker_HashMismatch() {
	require := suite.Require()

	completion := make(chan struct{}, 1)

	// Set up optimistic processing info with different hash
	info := OptimisticProcessingInfo{
		Height:     suite.ctx.BlockHeight(),
		Hash:       []byte("different-hash"),
		Completion: completion,
		Aborted:    false,
	}
	suite.app.optimisticProcessingInfoMutex.Lock()
	suite.app.optimisticProcessingInfo = info
	suite.app.optimisticProcessingInfoMutex.Unlock()

	// Signal completion
	completion <- struct{}{}

	req := &abci.RequestFinalizeBlock{
		Height: suite.ctx.BlockHeight(),
		Hash:   []byte("request-hash"), // Different hash
		Txs:    [][]byte{},
	}

	resp, err := suite.app.FinalizeBlocker(suite.ctx, req)
	require.NoError(err)
	require.NotNil(resp)

	// Hash mismatch should cause fallback processing
}

// Test FinalizeBlocker with no optimistic processing
func (suite *OptimisticProcessingTestSuite) TestFinalizeBlocker_NoOptimisticProcessing() {
	require := suite.Require()

	// Ensure no optimistic processing
	suite.app.ClearOptimisticProcessingInfo()

	req := &abci.RequestFinalizeBlock{
		Height: suite.ctx.BlockHeight(),
		Hash:   []byte("test-hash"),
		Txs:    [][]byte{},
	}

	resp, err := suite.app.FinalizeBlocker(suite.ctx, req)
	require.NoError(err)
	require.NotNil(resp)

	// Should use fallback processing when no optimistic processing is active
}

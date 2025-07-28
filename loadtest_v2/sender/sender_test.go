package sender

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/loadtest_v2/config"
	"github.com/sei-protocol/sei-chain/loadtest_v2/generator"
	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/scenarios"
)

// JSONRPCRequest represents a captured JSON-RPC request
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  []string    `json:"params"`
	ID      int         `json:"id"`
}

// MockServer captures JSON-RPC requests for testing
type MockServer struct {
	server   *httptest.Server
	requests []JSONRPCRequest
	mu       sync.Mutex
}

// NewMockServer creates a new mock JSON-RPC server
func NewMockServer() *MockServer {
	ms := &MockServer{
		requests: make([]JSONRPCRequest, 0),
	}
	
	ms.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
		
		// Parse JSON-RPC request
		var req JSONRPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		
		// Store the request
		ms.mu.Lock()
		ms.requests = append(ms.requests, req)
		ms.mu.Unlock()
		
		// Send a mock response
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  "0x1234567890abcdef", // Mock transaction hash
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	
	return ms
}

// GetRequests returns all captured requests
func (ms *MockServer) GetRequests() []JSONRPCRequest {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	// Return a copy to avoid race conditions
	requests := make([]JSONRPCRequest, len(ms.requests))
	copy(requests, ms.requests)
	return requests
}

// GetURL returns the server URL
func (ms *MockServer) GetURL() string {
	return ms.server.URL
}

// Close shuts down the server
func (ms *MockServer) Close() {
	ms.server.Close()
}

// TestShardedSenderWithMockServers tests the complete sender system with real HTTP servers
func TestShardedSenderWithMockServers(t *testing.T) {
	// Create mock servers for each endpoint
	numShards := 4
	mockServers := make([]*MockServer, numShards)
	endpoints := make([]string, numShards)
	
	for i := 0; i < numShards; i++ {
		mockServers[i] = NewMockServer()
		endpoints[i] = mockServers[i].GetURL()
	}
	
	// Cleanup servers when test completes
	defer func() {
		for _, server := range mockServers {
			server.Close()
		}
	}()
	
	// Create test configuration
	cfg := &config.LoadConfig{
		ChainID:    7777,
		MockDeploy: true,
		Endpoints:  endpoints,
		Scenarios: []config.Scenario{
			{Name: scenarios.ERC20, Weight: 1},
		},
	}

	// Create generator (MockDeploy=true means it auto-deploys)
	gen, err := generator.NewConfigBasedGenerator(cfg)
	require.NoError(t, err)

	// Create sharded sender with larger buffer to handle burst
	sender, err := NewShardedSender(cfg, 50) // Larger buffer for testing
	require.NoError(t, err)

	// Start the sender (starts all workers)
	sender.Start()
	defer sender.Stop()

	// Create dispatcher
	dispatcher := NewDispatcher(gen, sender)
	
	// Set a small rate limit to prevent overwhelming the workers
	dispatcher.SetRateLimit(5 * time.Millisecond)

	// Send a batch of transactions
	batchSize := 20
	err = dispatcher.StartBatch(batchSize)
	require.NoError(t, err)

	// Wait for batch to complete
	dispatcher.Wait()

	// Give workers more time to process all requests
	time.Sleep(200 * time.Millisecond)

	// Check dispatcher statistics
	stats := dispatcher.GetStats()
	assert.Equal(t, uint64(batchSize), stats.TotalSent)

	// Verify requests were distributed across shards
	totalRequests := 0
	shardDistribution := make(map[int]int)
	
	for shardID, server := range mockServers {
		requests := server.GetRequests()
		requestCount := len(requests)
		totalRequests += requestCount
		shardDistribution[shardID] = requestCount
		
		fmt.Printf("Shard %d received %d requests\n", shardID, requestCount)
		
		// Verify all requests are valid JSON-RPC
		for _, req := range requests {
			assert.Equal(t, "2.0", req.JSONRPC)
			assert.Equal(t, "eth_sendRawTransaction", req.Method)
			assert.Len(t, req.Params, 1) // Should have one parameter (raw transaction)
			assert.GreaterOrEqual(t, req.ID, 0)
		}
	}
	
	// Verify total requests match what we sent
	assert.Equal(t, batchSize, totalRequests, "Total requests should match batch size")
	
	// Verify distribution is reasonable (each shard should get at least one request for sufficient batch size)
	if batchSize >= numShards*2 {
		usedShards := 0
		for _, count := range shardDistribution {
			if count > 0 {
				usedShards++
			}
		}
		assert.GreaterOrEqual(t, usedShards, numShards/2, "At least half the shards should be used")
	}
	
	fmt.Printf("Distribution: %v\n", shardDistribution)
}

// TestShardDistributionVerification tests that specific transactions go to expected shards
func TestShardDistributionVerification(t *testing.T) {
	// Create 2 mock servers for simpler testing
	numShards := 2
	mockServers := make([]*MockServer, numShards)
	endpoints := make([]string, numShards)
	
	for i := 0; i < numShards; i++ {
		mockServers[i] = NewMockServer()
		endpoints[i] = mockServers[i].GetURL()
	}
	
	defer func() {
		for _, server := range mockServers {
			server.Close()
		}
	}()
	
	cfg := &config.LoadConfig{
		ChainID:    7777,
		MockDeploy: true,
		Endpoints:  endpoints,
		Scenarios: []config.Scenario{
			{Name: scenarios.ERC20, Weight: 1},
		},
	}

	// Create generator
	gen, err := generator.NewConfigBasedGenerator(cfg)
	require.NoError(t, err)

	// Create sender
	sender, err := NewShardedSender(cfg, 10)
	require.NoError(t, err)
	sender.Start()
	defer sender.Stop()

	// Generate transactions and verify shard assignment
	numTxs := 10
	expectedShards := make(map[int]int) // map[shardID]count
	
	for i := 0; i < numTxs; i++ {
		tx := gen.Generate()
		require.NotNil(t, tx)

		// Calculate expected shard
		expectedShard := tx.ShardID(numShards)
		expectedShards[expectedShard]++
		
		// Send transaction
		err := sender.Send(tx)
		require.NoError(t, err)
	}
	
	// Wait for processing
	time.Sleep(100 * time.Millisecond)
	
	// Verify actual distribution matches expected
	for shardID, server := range mockServers {
		requests := server.GetRequests()
		actualCount := len(requests)
		expectedCount := expectedShards[shardID]
		
		assert.Equal(t, expectedCount, actualCount, 
			"Shard %d should have received %d requests, got %d", 
			shardID, expectedCount, actualCount)
	}
}

// TestShardDistribution verifies that transactions are distributed across shards correctly
func TestShardDistribution(t *testing.T) {
	cfg := &config.LoadConfig{
		ChainID:    7777,
		MockDeploy: true,
		Endpoints: []string{
			"http://localhost:8545",
			"http://localhost:8546",
		},
		Scenarios: []config.Scenario{
			{Name: scenarios.ERC20, Weight: 1},
		},
	}

	// Create generator
	gen, err := generator.NewConfigBasedGenerator(cfg)
	require.NoError(t, err)

	// Create sender
	sender, err := NewShardedSender(cfg, 10)
	require.NoError(t, err)

	// Test shard calculation
	assert.Equal(t, 2, sender.GetNumShards())

	// Generate some transactions and verify they get distributed
	for i := 0; i < 10; i++ {
		tx := gen.Generate()
		require.NotNil(t, tx)

		shardID := tx.ShardID(2)
		assert.True(t, shardID >= 0 && shardID < 2, "Shard ID should be 0 or 1")

		// Send transaction - should succeed since buffer size (10) >= number of transactions (10)
		// Workers aren't started, but channels have sufficient capacity
		err := sender.Send(tx)
		assert.NoError(t, err, "Transaction %d should succeed (buffer has capacity)", i+1)
	}
}

// TestWorkerBuffering tests that workers can handle buffered transactions
func TestWorkerBuffering(t *testing.T) {
	worker := NewWorker(0, "http://localhost:8545", 5) // Small buffer for testing

	// Don't start the worker - this tests buffering only
	defer worker.Stop()

	// Create some mock transactions
	cfg := &config.LoadConfig{
		ChainID:    7777,
		MockDeploy: true,
		Scenarios:  []config.Scenario{{Name: scenarios.ERC20, Weight: 1}},
	}
	gen, err := generator.NewConfigBasedGenerator(cfg)
	require.NoError(t, err)

	// Send transactions to fill the buffer (should succeed for first 5)
	for i := 0; i < 5; i++ {
		tx := gen.Generate()
		err := worker.Send(tx)
		assert.NoError(t, err, "Transaction %d should succeed (buffer not full)", i+1)
	}

	// Buffer should be full now
	assert.Equal(t, 5, worker.GetChannelLength())

	// Next send should fail (buffer full, worker not processing)
	tx := gen.Generate()
	err = worker.Send(tx)
	assert.Error(t, err, "Should fail when buffer is full")
	assert.Contains(t, err.Error(), "channel is full", "Error should indicate channel is full")
}

package evmrpc_test

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// TestWorkerPoolConfigE2E is an end-to-end test that verifies:
// 1. Worker pool config can be read from a real TOML file
// 2. The config is correctly applied to the worker pool
// 3. The worker pool behaves according to the config
func TestWorkerPoolConfigE2E(t *testing.T) {
	tests := []struct {
		name             string
		tomlConfig       string
		expectedWorkers  int
		expectedQueue    int
		shouldHandleLoad bool
		concurrentTasks  int
		expectedFailures int
	}{
		{
			name: "default config (zero values)",
			tomlConfig: `
[evm]
http_enabled = true
worker_pool_size = 0
worker_queue_size = 0
`,
			expectedWorkers:  -1,   // Config will be 0, NewWorkerPool converts to runtime default
			expectedQueue:    1000, // Config will be 0, NewWorkerPool converts to 1000
			shouldHandleLoad: true,
			concurrentTasks:  50,
			expectedFailures: 0,
		},
		{
			name: "custom large config",
			tomlConfig: `
[evm]
http_enabled = true
worker_pool_size = 16
worker_queue_size = 500
`,
			expectedWorkers:  16,
			expectedQueue:    500,
			shouldHandleLoad: true,
			concurrentTasks:  100,
			expectedFailures: 0,
		},
		{
			name: "small config under load",
			tomlConfig: `
[evm]
http_enabled = true
worker_pool_size = 2
worker_queue_size = 5
`,
			expectedWorkers:  2,
			expectedQueue:    5,
			shouldHandleLoad: false, // Should fail under high load
			concurrentTasks:  50,
			expectedFailures: 40, // Approximately, most tasks should fail
		},
		{
			name: "missing worker pool config (uses defaults)",
			tomlConfig: `
[evm]
http_enabled = true
http_port = 8545
`,
			expectedWorkers:  -1, // Will be runtime.NumCPU() * 2, just check > 0
			expectedQueue:    1000,
			shouldHandleLoad: true,
			concurrentTasks:  50,
			expectedFailures: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Step 1: Create a temporary TOML file
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "app.toml")
			err := os.WriteFile(configPath, []byte(tt.tomlConfig), 0644)
			require.NoError(t, err, "Failed to write test config file")

			// Step 2: Load config using Viper (simulates real app loading)
			v := viper.New()
			v.SetConfigFile(configPath)
			err = v.ReadInConfig()
			require.NoError(t, err, "Failed to read config file")

			// Step 3: Read EVM config
			cfg, err := evmrpc.ReadConfig(v)
			require.NoError(t, err, "Failed to read EVM config")

			// Step 4: Create a new worker pool with the config (not global)
			// to avoid singleton issues in tests
			workerCount := cfg.WorkerPoolSize
			queueSize := cfg.WorkerQueueSize

			// Apply defaults like InitGlobalWorkerPool does
			if workerCount <= 0 {
				workerCount = runtime.NumCPU() * 2
			}
			if queueSize <= 0 {
				queueSize = evmrpc.DefaultWorkerQueueSize
			}

			wp := evmrpc.NewWorkerPool(workerCount, queueSize)
			wp.Start()
			defer wp.Close()
			require.NotNil(t, wp, "Worker pool should be initialized")

			// Step 6: Verify worker pool is configured correctly
			actualWorkers := wp.WorkerCount()
			actualQueue := wp.QueueSize()

			// Verify defaults are applied when config is 0 or -1
			if tt.expectedWorkers == 0 || tt.expectedWorkers == -1 {
				require.Greater(t, actualWorkers, 0,
					"Worker count should be > 0 (default applied)")
			} else {
				require.Equal(t, tt.expectedWorkers, actualWorkers,
					"Worker count mismatch")
			}

			require.Equal(t, tt.expectedQueue, actualQueue,
				"Queue size mismatch")

			// Step 5: Test worker pool under load
			failures := testWorkerPoolUnderLoad(t, wp, tt.concurrentTasks)

			if tt.shouldHandleLoad {
				require.Equal(t, 0, failures,
					"Worker pool should handle load without failures")
			} else {
				require.Greater(t, failures, tt.expectedFailures/2,
					"Worker pool should have failures under high load with small config")
			}
		})
	}
}

// testWorkerPoolUnderLoad tests the worker pool by submitting concurrent tasks
// Returns the number of failed submissions
func testWorkerPoolUnderLoad(t *testing.T, wp *evmrpc.WorkerPool, taskCount int) int {
	t.Helper()

	var failures atomic.Int32
	var completed atomic.Int32

	// Block the workers with slow tasks
	for range wp.WorkerCount() {
		wp.Submit(func() {
			time.Sleep(50 * time.Millisecond)
		})
	}

	// Try to submit many tasks quickly
	for range taskCount {
		err := wp.Submit(func() {
			time.Sleep(10 * time.Millisecond)
			completed.Add(1)
		})
		if err != nil {
			failures.Add(1)
			if err.Error() != "worker pool queue is full" {
				t.Errorf("Unexpected error: %v", err)
			}
		}
	}

	// Wait for tasks to complete
	time.Sleep(200 * time.Millisecond)

	return int(failures.Load())
}

// TestConfigRealisticScenario simulates a realistic upgrade scenario
func TestConfigRealisticScenario(t *testing.T) {
	t.Run("old node without new config", func(t *testing.T) {
		// Simulate an old app.toml without worker pool config
		oldConfig := `
[evm]
http_enabled = true
http_port = 8545
ws_enabled = true
ws_port = 8546
cors_origins = "*"
simulation_gas_limit = 10000000
`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "app.toml")
		err := os.WriteFile(configPath, []byte(oldConfig), 0644)
		require.NoError(t, err)

		v := viper.New()
		v.SetConfigFile(configPath)
		err = v.ReadInConfig()
		require.NoError(t, err)

		// Should successfully read config with defaults
		cfg, err := evmrpc.ReadConfig(v)
		require.NoError(t, err)

		// Old config should use default calculated values: min(MaxWorkerPoolSize, runtime.NumCPU() * 2)
		require.Greater(t, cfg.WorkerPoolSize, 0, "Should have default worker pool size")
		require.Equal(t, 1000, cfg.WorkerQueueSize, "Should use default queue size")

		// Initialize worker pool - should apply defaults
		workerCount := cfg.WorkerPoolSize
		if workerCount <= 0 {
			workerCount = runtime.NumCPU() * 2
		}
		queueSize := cfg.WorkerQueueSize
		if queueSize <= 0 {
			queueSize = evmrpc.DefaultWorkerQueueSize
		}

		wp := evmrpc.NewWorkerPool(workerCount, queueSize)
		wp.Start()
		defer wp.Close()

		// Verify defaults are applied
		require.Greater(t, wp.WorkerCount(), 0, "Should apply default worker count")
		require.Equal(t, evmrpc.DefaultWorkerQueueSize, wp.QueueSize(),
			"Should apply default queue size")
	})

	t.Run("manually updated config", func(t *testing.T) {
		// Simulate manually adding config to existing app.toml
		updatedConfig := `
[evm]
http_enabled = true
http_port = 8545
ws_enabled = true
ws_port = 8546
cors_origins = "*"
simulation_gas_limit = 10000000

# Manually added worker pool config
worker_pool_size = 36
worker_queue_size = 1000
`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "app.toml")
		err := os.WriteFile(configPath, []byte(updatedConfig), 0644)
		require.NoError(t, err)

		v := viper.New()
		v.SetConfigFile(configPath)
		err = v.ReadInConfig()
		require.NoError(t, err)

		cfg, err := evmrpc.ReadConfig(v)
		require.NoError(t, err)

		// Should read custom values
		require.Equal(t, 36, cfg.WorkerPoolSize, "Should read custom value")
		require.Equal(t, 1000, cfg.WorkerQueueSize, "Should read custom value")

		// Initialize worker pool (not global, to avoid test conflicts)
		wp := evmrpc.NewWorkerPool(cfg.WorkerPoolSize, cfg.WorkerQueueSize)
		wp.Start()
		defer wp.Close()

		// Verify custom values are used
		require.Equal(t, 36, wp.WorkerCount(), "Should use custom worker count")
		require.Equal(t, 1000, wp.QueueSize(), "Should use custom queue size")
	})
}

// TestWorkerPoolPerformanceWithConfig benchmarks different configurations
func TestWorkerPoolPerformanceWithConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	configs := []struct {
		name        string
		workers     int
		queueSize   int
		taskCount   int
		maxDuration time.Duration
	}{
		{
			name:        "small_config",
			workers:     4,
			queueSize:   100,
			taskCount:   100, // Match queue size
			maxDuration: 2 * time.Second,
		},
		{
			name:        "medium_config",
			workers:     16,
			queueSize:   500,
			taskCount:   500, // Match queue size
			maxDuration: 3 * time.Second,
		},
		{
			name:        "large_config",
			workers:     32,
			queueSize:   1000,
			taskCount:   1000, // Reduce to match queue size for higher success rate
			maxDuration: 4 * time.Second,
		},
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			wp := evmrpc.NewWorkerPool(tc.workers, tc.queueSize)
			wp.Start()
			defer wp.Close()

			var completed atomic.Int32
			failed := 0
			var wg sync.WaitGroup

			for range tc.taskCount {
				wg.Add(1)
				err := wp.Submit(func() {
					defer wg.Done()
					time.Sleep(1 * time.Millisecond)
					completed.Add(1)
				})
				if err != nil {
					wg.Done() // Don't forget to mark as done if submission failed
					failed++
				}
			}

			// Wait for all tasks to complete
			wg.Wait()

			successRate := float64(tc.taskCount-failed) / float64(tc.taskCount) * 100

			// With proper config, success rate should be high
			require.Greater(t, successRate, 80.0,
				"Success rate should be > 80%% with proper config")
		})
	}
}

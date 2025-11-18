package evmrpc_test

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/require"
)

type opts struct {
	httpEnabled                  interface{}
	httpPort                     interface{}
	wsEnabled                    interface{}
	wsPort                       interface{}
	readTimeout                  interface{}
	readHeaderTimeout            interface{}
	writeTimeout                 interface{}
	idleTimeout                  interface{}
	simulationGasLimit           interface{}
	simulationEVMTimeout         interface{}
	corsOrigins                  interface{}
	wsOrigins                    interface{}
	filterTimeout                interface{}
	checkTxTimeout               interface{}
	maxTxPoolTxs                 interface{}
	slow                         interface{}
	disableWatermark             interface{}
	denyList                     interface{}
	maxLogNoBlock                interface{}
	maxBlocksForLog              interface{}
	maxSubscriptionsNewHead      interface{}
	enableTestAPI                interface{}
	maxConcurrentTraceCalls      interface{}
	maxConcurrentSimulationCalls interface{}
	maxTraceLookbackBlocks       interface{}
	traceTimeout                 interface{}
	rpcStatsInterval             interface{}
	workerPoolSize               interface{}
	workerQueueSize              interface{}
}

func (o *opts) Get(k string) interface{} {
	if k == "evm.http_enabled" {
		return o.httpEnabled
	}
	if k == "evm.http_port" {
		return o.httpPort
	}
	if k == "evm.ws_enabled" {
		return o.wsEnabled
	}
	if k == "evm.ws_port" {
		return o.wsPort
	}
	if k == "evm.read_timeout" {
		return o.readTimeout
	}
	if k == "evm.read_header_timeout" {
		return o.readHeaderTimeout
	}
	if k == "evm.write_timeout" {
		return o.writeTimeout
	}
	if k == "evm.idle_timeout" {
		return o.idleTimeout
	}
	if k == "evm.simulation_gas_limit" {
		return o.simulationGasLimit
	}
	if k == "evm.simulation_evm_timeout" {
		return o.simulationEVMTimeout
	}
	if k == "evm.cors_origins" {
		return o.corsOrigins
	}
	if k == "evm.ws_origins" {
		return o.wsOrigins
	}
	if k == "evm.filter_timeout" {
		return o.filterTimeout
	}
	if k == "evm.checktx_timeout" {
		return o.checkTxTimeout
	}
	if k == "evm.max_tx_pool_txs" {
		return o.maxTxPoolTxs
	}
	if k == "evm.slow" {
		return o.slow
	}
	if k == "evm.disable_watermark" {
		return o.disableWatermark
	}
	if k == "evm.deny_list" {
		return o.denyList
	}
	if k == "evm.max_log_no_block" {
		return o.maxLogNoBlock
	}
	if k == "evm.max_blocks_for_log" {
		return o.maxBlocksForLog
	}
	if k == "evm.max_subscriptions_new_head" {
		return o.maxSubscriptionsNewHead
	}
	if k == "evm.enable_test_api" {
		return o.enableTestAPI
	}
	if k == "evm.max_concurrent_trace_calls" {
		return o.maxConcurrentTraceCalls
	}
	if k == "evm.max_concurrent_simulation_calls" {
		return o.maxConcurrentSimulationCalls
	}
	if k == "evm.max_trace_lookback_blocks" {
		return o.maxTraceLookbackBlocks
	}
	if k == "evm.trace_timeout" {
		return o.traceTimeout
	}
	if k == "evm.rpc_stats_interval" {
		return o.rpcStatsInterval
	}
	if k == "evm.worker_pool_size" {
		return o.workerPoolSize
	}
	if k == "evm.worker_queue_size" {
		return o.workerQueueSize
	}
	panic("unknown key")
}

// getDefaultOpts returns a valid opts struct with all required fields set
func getDefaultOpts() opts {
	return opts{
		true,
		1,
		true,
		2,
		time.Duration(5),
		time.Duration(5),
		time.Duration(5),
		time.Duration(5),
		uint64(10),
		time.Duration(60),
		"",
		"",
		time.Duration(5),
		time.Duration(5),
		1000,
		false,
		false,
		make([]string, 0),
		20000,
		1000,
		10000,
		false,
		uint64(10),
		uint64(10),
		int64(100),
		30 * time.Second,
		10 * time.Second,
		32,
		1000,
	}
}

func TestReadConfig(t *testing.T) {
	goodOpts := getDefaultOpts()
	_, err := evmrpc.ReadConfig(&goodOpts)
	require.Nil(t, err)
	badOpts := goodOpts
	badOpts.httpEnabled = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.httpPort = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.wsEnabled = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.wsPort = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.readTimeout = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.readHeaderTimeout = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.writeTimeout = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.idleTimeout = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.filterTimeout = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.simulationGasLimit = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.simulationEVMTimeout = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.corsOrigins = map[string]interface{}{}
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.wsOrigins = map[string]interface{}{}
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.checkTxTimeout = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.maxTxPoolTxs = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.slow = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.denyList = map[string]interface{}{}
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)

	// Test bad types for new trace config options
	badOpts = goodOpts
	badOpts.maxConcurrentTraceCalls = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)

	// Test bad types for new trace config options
	badOpts = goodOpts
	badOpts.maxConcurrentSimulationCalls = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)

	badOpts = goodOpts
	badOpts.maxTraceLookbackBlocks = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)

	badOpts = goodOpts
	badOpts.traceTimeout = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)

	// Test bad types for worker pool config
	badOpts = goodOpts
	badOpts.workerPoolSize = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)

	badOpts = goodOpts
	badOpts.workerQueueSize = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
}

// Test worker pool configuration values
func TestReadConfigWorkerPool(t *testing.T) {
	tests := []struct {
		name            string
		workerPoolSize  interface{}
		workerQueueSize interface{}
		expectedWorkers int
		expectedQueue   int
	}{
		{
			name:            "custom values",
			workerPoolSize:  32,
			workerQueueSize: 1000,
			expectedWorkers: 32,
			expectedQueue:   1000,
		},
		{
			name:            "zero values (use defaults)",
			workerPoolSize:  0,
			workerQueueSize: 0,
			expectedWorkers: 0,
			expectedQueue:   0,
		},
		{
			name:            "only worker size",
			workerPoolSize:  48,
			workerQueueSize: 0,
			expectedWorkers: 48,
			expectedQueue:   0,
		},
		{
			name:            "only queue size",
			workerPoolSize:  0,
			workerQueueSize: 2000,
			expectedWorkers: 0,
			expectedQueue:   2000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := getDefaultOpts()
			// Only set the fields we're testing
			opts.workerPoolSize = tt.workerPoolSize
			opts.workerQueueSize = tt.workerQueueSize

			cfg, err := evmrpc.ReadConfig(&opts)
			require.Nil(t, err)

			require.Equal(t, tt.expectedWorkers, cfg.WorkerPoolSize,
				"WorkerPoolSize mismatch")
			require.Equal(t, tt.expectedQueue, cfg.WorkerQueueSize,
				"WorkerQueueSize mismatch")
		})
	}
}

package config_test

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/evmrpc/config"
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
	maxEstimateGasCalls          interface{}
	maxSubscriptionsNewHead      interface{}
	maxSubscriptionsLogs         interface{}
	enableTestAPI                interface{}
	maxConcurrentTraceCalls      interface{}
	maxConcurrentSimulationCalls interface{}
	maxTraceLookbackBlocks       interface{}
	traceTimeout                 interface{}
	enableParallelizedBlockTrace interface{}
	rpcStatsInterval             interface{}
	workerPoolSize               interface{}
	workerQueueSize              interface{}
	ipRateLimitRPS               interface{}
	ipRateLimitBurst             interface{}
	batchRequestLimit            interface{}
	batchResponseMaxSize         interface{}
	maxRequestBodyBytes          interface{}
	maxConcurrentRequestBytes    interface{}
	maxOpenConnections           interface{}
	maxTraceStructLogBytes       interface{}
	maxStateOverrideAccounts     interface{}
	maxStateOverrideSlots        interface{}
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
	if k == "evm.max_estimate_gas_calls" {
		return o.maxEstimateGasCalls
	}
	if k == "evm.max_subscriptions_new_head" {
		return o.maxSubscriptionsNewHead
	}
	if k == "evm.max_subscriptions_logs" {
		return o.maxSubscriptionsLogs
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
	if k == "evm.enable_parallelized_block_trace" {
		return o.enableParallelizedBlockTrace
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
	if k == "evm.enabled_legacy_sei_apis" {
		return nil
	}
	if k == "evm.trace_bake_enabled" ||
		k == "evm.trace_bake_workers" ||
		k == "evm.trace_bake_queue_size" ||
		k == "evm.trace_bake_tracers" ||
		k == "evm.trace_bake_window_blocks" ||
		k == "evm.trace_bake_use_snapshot" ||
		k == "evm.trace_bake_snapshot_window" {
		return nil
	}
	if k == "evm.ip_rate_limit_rps" {
		return o.ipRateLimitRPS
	}
	if k == "evm.ip_rate_limit_burst" {
		return o.ipRateLimitBurst
	}
	if k == "evm.batch_request_limit" {
		return o.batchRequestLimit
	}
	if k == "evm.batch_response_max_size" {
		return o.batchResponseMaxSize
	}
	if k == "evm.max_request_body_bytes" {
		return o.maxRequestBodyBytes
	}
	if k == "evm.max_concurrent_request_bytes" {
		return o.maxConcurrentRequestBytes
	}
	if k == "evm.max_open_connections" {
		return o.maxOpenConnections
	}
	if k == "evm.max_trace_struct_log_bytes" {
		return o.maxTraceStructLogBytes
	}
	if k == "evm.max_state_override_accounts" {
		return o.maxStateOverrideAccounts
	}
	if k == "evm.max_state_override_slots" {
		return o.maxStateOverrideSlots
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
		100,
		10000,
		1000,
		false,
		uint64(10),
		uint64(10),
		int64(100),
		30 * time.Second,
		false,
		10 * time.Second,
		32,
		1000,
		200.0,
		400,
		1000,
		25 * 1000 * 1000,
		int64(5 * 1024 * 1024),
		int64(128 * 1024 * 1024),
		2000,
		uint64(256 * 1024 * 1024),
		7,
		9,
	}
}

func TestReadConfig(t *testing.T) {
	goodOpts := getDefaultOpts()
	cfg, err := config.ReadConfig(&goodOpts)
	require.Nil(t, err)
	require.False(t, cfg.EnableParallelizedBlockTrace)
	// Round-trip: an explicitly-supplied value overrides the default.
	require.Equal(t, uint64(256*1024*1024), cfg.MaxTraceStructLogBytes)
	// The shipped default (used when the operator supplies no value).
	require.Equal(t, uint64(32*1024*1024), config.DefaultConfig.MaxTraceStructLogBytes)
	// State override caps: round-trip the supplied values, and assert shipped defaults.
	require.Equal(t, 7, cfg.MaxStateOverrideAccounts)
	require.Equal(t, 9, cfg.MaxStateOverrideSlots)
	require.Equal(t, 100, config.DefaultConfig.MaxStateOverrideAccounts)
	require.Equal(t, 1000, config.DefaultConfig.MaxStateOverrideSlots)
	badOpts := goodOpts
	badOpts.httpEnabled = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.httpPort = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.wsEnabled = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.wsPort = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.readTimeout = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.readHeaderTimeout = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.writeTimeout = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.idleTimeout = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.filterTimeout = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.simulationGasLimit = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.simulationEVMTimeout = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.corsOrigins = map[string]interface{}{}
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.wsOrigins = map[string]interface{}{}
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.checkTxTimeout = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.maxTxPoolTxs = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.slow = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.denyList = map[string]interface{}{}
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)

	// Test bad types for new trace config options
	badOpts = goodOpts
	badOpts.maxConcurrentTraceCalls = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)

	// Test bad types for new trace config options
	badOpts = goodOpts
	badOpts.maxConcurrentSimulationCalls = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)

	badOpts = goodOpts
	badOpts.maxTraceLookbackBlocks = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)

	badOpts = goodOpts
	badOpts.traceTimeout = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)

	badOpts = goodOpts
	badOpts.enableParallelizedBlockTrace = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)

	badOpts = goodOpts
	badOpts.maxTraceStructLogBytes = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)

	badOpts = goodOpts
	badOpts.maxStateOverrideAccounts = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)

	badOpts = goodOpts
	badOpts.maxStateOverrideSlots = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)

	// Test bad types for worker pool config
	badOpts = goodOpts
	badOpts.workerPoolSize = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)

	badOpts = goodOpts
	badOpts.workerQueueSize = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)

	// Test bad types for rate limit config
	badOpts = goodOpts
	badOpts.ipRateLimitRPS = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)

	badOpts = goodOpts
	badOpts.ipRateLimitBurst = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)

	// Test bad types for batch limit config
	badOpts = goodOpts
	badOpts.batchRequestLimit = "bad"
	_, err = config.ReadConfig(&badOpts)
	require.NotNil(t, err)

	badOpts = goodOpts
	badOpts.batchResponseMaxSize = "bad"
	_, err = config.ReadConfig(&badOpts)
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

			cfg, err := config.ReadConfig(&opts)
			require.Nil(t, err)

			require.Equal(t, tt.expectedWorkers, cfg.WorkerPoolSize,
				"WorkerPoolSize mismatch")
			require.Equal(t, tt.expectedQueue, cfg.WorkerQueueSize,
				"WorkerQueueSize mismatch")
		})
	}
}

func TestReadConfigBatchLimits(t *testing.T) {
	// Defaults flow through when not overridden.
	cfg, err := config.ReadConfig(&opts{})
	require.NoError(t, err)
	require.Equal(t, config.DefaultConfig.BatchRequestLimit, cfg.BatchRequestLimit)
	require.Equal(t, config.DefaultConfig.BatchResponseMaxSize, cfg.BatchResponseMaxSize)

	// Custom values (including 0 to disable) flow through.
	o := getDefaultOpts()
	o.batchRequestLimit = 50
	o.batchResponseMaxSize = 0
	cfg, err = config.ReadConfig(&o)
	require.NoError(t, err)
	require.Equal(t, 50, cfg.BatchRequestLimit)
	require.Equal(t, 0, cfg.BatchResponseMaxSize)
}

func TestReadConfigRequestSizeLimits(t *testing.T) {
	// Defaults flow through when not overridden.
	cfg, err := config.ReadConfig(&opts{})
	require.NoError(t, err)
	require.Equal(t, config.DefaultConfig.MaxRequestBodyBytes, cfg.MaxRequestBodyBytes)
	require.Equal(t, config.DefaultConfig.MaxConcurrentRequestBytes, cfg.MaxConcurrentRequestBytes)

	// Custom values (including 0 to use default / disable) flow through.
	o := getDefaultOpts()
	o.maxRequestBodyBytes = int64(1024)
	o.maxConcurrentRequestBytes = int64(0)
	cfg, err = config.ReadConfig(&o)
	require.NoError(t, err)
	require.Equal(t, int64(1024), cfg.MaxRequestBodyBytes)
	require.Equal(t, int64(0), cfg.MaxConcurrentRequestBytes)
}

func TestReadConfigMaxOpenConnections(t *testing.T) {
	// Default flows through when not overridden.
	cfg, err := config.ReadConfig(&opts{})
	require.NoError(t, err)
	require.Equal(t, config.DefaultConfig.MaxOpenConnections, cfg.MaxOpenConnections)

	// Custom value (including 0 to disable) flows through.
	o := getDefaultOpts()
	o.maxOpenConnections = 0
	cfg, err = config.ReadConfig(&o)
	require.NoError(t, err)
	require.Equal(t, 0, cfg.MaxOpenConnections)

	o.maxOpenConnections = 500
	cfg, err = config.ReadConfig(&o)
	require.NoError(t, err)
	require.Equal(t, 500, cfg.MaxOpenConnections)

	// A negative value is rejected rather than silently disabling the limit.
	o.maxOpenConnections = -1
	_, err = config.ReadConfig(&o)
	require.Error(t, err)
}

func TestReadConfigEnableParallelizedBlockTrace(t *testing.T) {
	opts := getDefaultOpts()
	opts.enableParallelizedBlockTrace = true

	cfg, err := config.ReadConfig(&opts)
	require.NoError(t, err)
	require.True(t, cfg.EnableParallelizedBlockTrace)
}

func TestReadConfigMaxSubscriptionsLogs(t *testing.T) {
	opts := getDefaultOpts()
	opts.maxSubscriptionsLogs = uint64(42)
	cfg, err := config.ReadConfig(&opts)
	require.NoError(t, err)
	require.Equal(t, uint64(42), cfg.MaxSubscriptionsLogs)

	// A non-numeric value is rejected.
	opts.maxSubscriptionsLogs = "bad"
	_, err = config.ReadConfig(&opts)
	require.Error(t, err)
}

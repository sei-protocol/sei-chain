package evmrpc

import (
	"testing"
	"time"

	"github.com/spf13/viper"
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
	flushReceiptSync             interface{}
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
}

func (o *opts) Get(k string) interface{} {
	switch k {
	case "evm.http_enabled":
		return o.httpEnabled
	case "evm.http_port":
		return o.httpPort
	case "evm.ws_enabled":
		return o.wsEnabled
	case "evm.ws_port":
		return o.wsPort
	case "evm.read_timeout":
		return o.readTimeout
	case "evm.read_header_timeout":
		return o.readHeaderTimeout
	case "evm.write_timeout":
		return o.writeTimeout
	case "evm.idle_timeout":
		return o.idleTimeout
	case "evm.simulation_gas_limit":
		return o.simulationGasLimit
	case "evm.simulation_evm_timeout":
		return o.simulationEVMTimeout
	case "evm.cors_origins":
		return o.corsOrigins
	case "evm.ws_origins":
		return o.wsOrigins
	case "evm.filter_timeout":
		return o.filterTimeout
	case "evm.checktx_timeout":
		return o.checkTxTimeout
	case "evm.max_tx_pool_txs":
		return o.maxTxPoolTxs
	case "evm.slow":
		return o.slow
	case "evm.flush_receipt_sync":
		return o.flushReceiptSync
	case "evm.deny_list":
		return o.denyList
	case "evm.max_log_no_block":
		return o.maxLogNoBlock
	case "evm.max_blocks_for_log":
		return o.maxBlocksForLog
	case "evm.max_subscriptions_new_head":
		return o.maxSubscriptionsNewHead
	case "evm.enable_test_api":
		return o.enableTestAPI
	case "evm.max_concurrent_trace_calls":
		return o.maxConcurrentTraceCalls
	case "evm.max_concurrent_simulation_calls":
		return o.maxConcurrentSimulationCalls
	case "evm.max_trace_lookback_blocks":
		return o.maxTraceLookbackBlocks
	case "evm.trace_timeout":
		return o.traceTimeout
	case "evm.rpc_stats_interval":
		return o.rpcStatsInterval
	default:
		panic("unknown key: " + k)
	}
}

func TestReadConfig_Viper(t *testing.T) {
	viper.Set("evm_rpc.rpc_address", "127.0.0.1:9545")
	cfg := ReadConfig()
	require.Equal(t, "127.0.0.1:9545", cfg.RPCAddress)
}

func TestReadConfig_Opts(t *testing.T) {
	goodOpts := opts{
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
	}

	_, err := ReadConfig(&goodOpts)
	require.Nil(t, err)

	tests := []struct {
		name    string
		mutate  func(*opts)
	}{
		{"bad httpEnabled", func(o *opts) { o.httpEnabled = "bad" }},
		{"bad httpPort", func(o *opts) { o.httpPort = "bad" }},
		{"bad wsEnabled", func(o *opts) { o.wsEnabled = "bad" }},
		{"bad wsPort", func(o *opts) { o.wsPort = "bad" }},
		{"bad readTimeout", func(o *opts) { o.readTimeout = "bad" }},
		{"bad readHeaderTimeout", func(o *opts) { o.readHeaderTimeout = "bad" }},
		{"bad writeTimeout", func(o *opts) { o.writeTimeout = "bad" }},
		{"bad idleTimeout", func(o *opts) { o.idleTimeout = "bad" }},
		{"bad filterTimeout", func(o *opts) { o.filterTimeout = "bad" }},
		{"bad simulationGasLimit", func(o *opts) { o.simulationGasLimit = "bad" }},
		{"bad simulationEVMTimeout", func(o *opts) { o.simulationEVMTimeout = "bad" }},
		{"bad corsOrigins", func(o *opts) { o.corsOrigins = map[string]interface{}{} }},
		{"bad wsOrigins", func(o *opts) { o.wsOrigins = map[string]interface{}{} }},
		{"bad checkTxTimeout", func(o *opts) { o.checkTxTimeout = "bad" }},
		{"bad maxTxPoolTxs", func(o *opts) { o.maxTxPoolTxs = "bad" }},
		{"bad slow", func(o *opts) { o.slow = "bad" }},
		{"bad denyList", func(o *opts) { o.denyList = map[string]interface{}{} }},
		{"bad maxConcurrentTraceCalls", func(o *opts) { o.maxConcurrentTraceCalls = "bad" }},
		{"bad maxConcurrentSimulationCalls", func(o *opts) { o.maxConcurrentSimulationCalls = "bad" }},
		{"bad maxTraceLookbackBlocks", func(o *opts) { o.maxTraceLookbackBlocks = "bad" }},
		{"bad traceTimeout", func(o *opts) { o.traceTimeout = "bad" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bad := goodOpts
			tt.mutate(&bad)
			_, err := ReadConfig(&bad)
			require.NotNil(t, err)
		})
	}
}

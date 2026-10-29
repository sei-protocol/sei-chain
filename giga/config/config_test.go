package config_test

import (
	"bytes"
	"testing"
	"text/template"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// TestReadConfigAbsentKeysKeepDefaults pins the section baseline: no [giga] section means the node
// runs what it ran before the section existed.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	cfg, err := gigaconfig.ReadConfig(configtest.AppOpts{})
	require.NoError(t, err, "an absent [giga] section must read cleanly")
	require.Equal(t, gigaconfig.DefaultConfig, cfg)
}

// TestDefaultsMatchTheStorageDefaults holds the section's storage defaults against the values
// sei-db builds when nothing configures it.
func TestDefaultsMatchTheStorageDefaults(t *testing.T) {
	gc := seidbconfig.DefaultStorageGarbageCollectorConfig()
	cp := seidbconfig.DefaultCheckpointConfig()
	s := gigaconfig.DefaultConfig.Storage
	require.True(t, s.Receipts)
	require.Equal(t, gc.RollbackWindow, s.RollbackWindow)
	require.Equal(t, gc.LookbackWindow, s.LookbackWindow)
	require.Equal(t, seidbconfig.DefaultStateStoreConfig().Backend, s.StateStoreBackend)
	require.Equal(t, gc.PruneInterval, s.PruneInterval)
	require.Equal(t, cp.TimeInterval, s.CheckpointTimeInterval)
	require.Equal(t, cp.BlockInterval, s.CheckpointBlockInterval)
}

// TestRPCDefaultsMatchTheV2EVMRPC holds the [giga.rpc] limits to the v2 EVM RPC's defaults, so
// both RPCs refuse the same load out of the box.
func TestRPCDefaultsMatchTheV2EVMRPC(t *testing.T) {
	v2 := evmrpcconfig.DefaultConfig
	r := gigaconfig.DefaultConfig.RPC
	require.Equal(t, v2.MaxSubscriptionsNewHead, r.MaxSubscriptionsNewHead)
	require.Equal(t, v2.MaxRequestBodyBytes, int64(r.MaxRequestBodyBytes))
	require.Equal(t, v2.MaxConcurrentRequestBytes, r.MaxConcurrentRequestBytes)
	require.Equal(t, v2.WSAdmissionTimeout, r.WSAdmissionTimeout)
	require.Equal(t, v2.BatchRequestLimit, r.BatchRequestLimit)
	require.Equal(t, v2.BatchResponseMaxSize, r.BatchResponseMaxSize)
	require.Equal(t, v2.MaxOpenConnections, r.MaxWSConnections)
	require.Equal(t, v2.MaxConcurrentTraceCalls, uint64(r.MaxConcurrentTraceCalls))
	require.Equal(t, v2.TraceTimeout, r.TraceTimeout)
	require.Equal(t, v2.MaxTraceStructLogBytes, uint64(r.MaxTraceStructLogBytes))
	require.Equal(t, uint64(100), r.MaxSubscriptionsPerConnection)
	require.Equal(t, v2.MaxEstimateGasCalls, r.MaxEstimateGasCalls)
}

// TestConfigTemplateRendersTheDefaults holds the app.toml template to the section: the rendered
// defaults read back as DefaultConfig.
func TestConfigTemplateRendersTheDefaults(t *testing.T) {
	tmpl := template.Must(template.New("giga").Parse(gigaconfig.ConfigTemplate))
	var rendered bytes.Buffer
	require.NoError(t, tmpl.Execute(&rendered, struct{ Giga gigaconfig.Config }{gigaconfig.DefaultConfig}))
	v := viper.New()
	v.SetConfigType("toml")
	require.NoError(t, v.ReadConfig(&rendered))
	cfg, err := gigaconfig.ReadConfig(v)
	require.NoError(t, err)
	require.Equal(t, gigaconfig.DefaultConfig, cfg)
}

func TestReadConfigReadsEveryKey(t *testing.T) {
	cfg, err := gigaconfig.ReadConfig(configtest.AppOpts{
		"giga.storage.mode":                         "full",
		"giga.storage.receipts":                     "false",
		"giga.storage.rollback_window":              "250",
		"giga.storage.lookback_window":              "-1",
		"giga.storage.state_store_backend":          "undolog",
		"giga.storage.prune_interval":               "90s",
		"giga.storage.checkpoint_time_interval":     "1h",
		"giga.storage.checkpoint_block_interval":    "5000",
		"giga.execution.min_gas_price":              "7",
		"giga.execution.occ_workers":                "3",
		"giga.execution.parse_workers":              "2",
		"giga.execution.block_result_pool_size":     "4",
		"giga.execution.evm_rpc_port":               "8600",
		"giga.execution.evm_ws_port":                "8601",
		"giga.rpc.max_subscriptions_new_head":       "20",
		"giga.rpc.max_subscriptions_per_connection": "5",
		"giga.rpc.max_request_body_bytes":           "2048",
		"giga.rpc.max_concurrent_request_bytes":     "4096",
		"giga.rpc.ws_admission_timeout":             "2s",
		"giga.rpc.batch_request_limit":              "10",
		"giga.rpc.batch_response_max_size":          "1000",
		"giga.rpc.max_ws_connections":               "7",
		"giga.rpc.max_estimate_gas_calls":           "9",
		"giga.rpc.max_concurrent_trace_calls":       "3",
		"giga.rpc.trace_timeout":                    "4s",
		"giga.rpc.max_trace_struct_log_bytes":       "512",
		"giga.state_sync.enabled":                   "true",
	})
	require.NoError(t, err)
	want := gigaconfig.Config{
		Storage: gigaconfig.StorageConfig{
			Mode:                    gigaconfig.StorageModeFull,
			Receipts:                false,
			RollbackWindow:          250,
			LookbackWindow:          -1,
			StateStoreBackend:       seidbconfig.PebbleDBUndoBackend,
			PruneInterval:           90 * time.Second,
			CheckpointTimeInterval:  time.Hour,
			CheckpointBlockInterval: 5000,
		},
		Execution: gigaconfig.ExecutionConfig{
			MinGasPrice:         7,
			OCCWorkers:          3,
			ParseWorkers:        2,
			BlockResultPoolSize: 4,
			EvmRpcPort:          8600,
			EvmWsPort:           8601,
		},
		RPC: gigaconfig.RPCConfig{
			MaxSubscriptionsNewHead:       20,
			MaxSubscriptionsPerConnection: 5,
			MaxRequestBodyBytes:           2048,
			MaxConcurrentRequestBytes:     4096,
			WSAdmissionTimeout:            2 * time.Second,
			BatchRequestLimit:             10,
			BatchResponseMaxSize:          1000,
			MaxWSConnections:              7,
			MaxEstimateGasCalls:           9,
			MaxConcurrentTraceCalls:       3,
			TraceTimeout:                  4 * time.Second,
			MaxTraceStructLogBytes:        512,
		},
		StateSync: gigaconfig.StateSyncConfig{
			Enabled: true,
		},
	}
	require.Equal(t, want, cfg)
}

func TestReadConfigRejectsUnusableValues(t *testing.T) {
	for name, opts := range map[string]configtest.AppOpts{
		"unknown mode":                    {"giga.storage.mode": "archive"},
		"non-boolean receipts":            {"giga.storage.receipts": "maybe"},
		"lookback below -1":               {"giga.storage.lookback_window": "-2"},
		"unknown backend":                 {"giga.storage.state_store_backend": "rocksdb"},
		"zero prune interval":             {"giga.storage.prune_interval": "0s"},
		"negative occ workers":            {"giga.execution.occ_workers": "-1"},
		"empty result pool":               {"giga.execution.block_result_pool_size": "0"},
		"non-numeric gas price":           {"giga.execution.min_gas_price": "cheap"},
		"negative block interval":         {"giga.storage.checkpoint_block_interval": "-1"},
		"evm rpc port zero":               {"giga.execution.evm_rpc_port": "0"},
		"evm rpc port too high":           {"giga.execution.evm_rpc_port": "65536"},
		"evm ws port zero":                {"giga.execution.evm_ws_port": "0"},
		"evm ws port too high":            {"giga.execution.evm_ws_port": "65536"},
		"evm ws port is rpc port":         {"giga.execution.evm_ws_port": "8545"},
		"negative subscription cap":       {"giga.rpc.max_subscriptions_new_head": "-1"},
		"non-numeric per-connection cap":  {"giga.rpc.max_subscriptions_per_connection": "many"},
		"zero request body size":          {"giga.rpc.max_request_body_bytes": "0"},
		"negative request body size":      {"giga.rpc.max_request_body_bytes": "-1"},
		"non-numeric request body size":   {"giga.rpc.max_request_body_bytes": "huge"},
		"negative request bytes":          {"giga.rpc.max_concurrent_request_bytes": "-1"},
		"negative admission timeout":      {"giga.rpc.ws_admission_timeout": "-1s"},
		"negative batch limit":            {"giga.rpc.batch_request_limit": "-1"},
		"negative batch response size":    {"giga.rpc.batch_response_max_size": "-1"},
		"negative ws connections":         {"giga.rpc.max_ws_connections": "-1"},
		"negative trace calls":            {"giga.rpc.max_concurrent_trace_calls": "-1"},
		"negative trace timeout":          {"giga.rpc.trace_timeout": "-1s"},
		"negative struct log bytes":       {"giga.rpc.max_trace_struct_log_bytes": "-1"},
		"non-numeric trace calls":         {"giga.rpc.max_concurrent_trace_calls": "many"},
		"non-duration trace timeout":      {"giga.rpc.trace_timeout": "later"},
		"non-numeric struct log bytes":    {"giga.rpc.max_trace_struct_log_bytes": "big"},
		"non-numeric request bytes":       {"giga.rpc.max_concurrent_request_bytes": "lots"},
		"non-duration admission timeout":  {"giga.rpc.ws_admission_timeout": "soon"},
		"non-numeric batch limit":         {"giga.rpc.batch_request_limit": "many"},
		"non-numeric batch response size": {"giga.rpc.batch_response_max_size": "big"},
		"non-numeric ws connections":      {"giga.rpc.max_ws_connections": "some"},
		"negative estimate gas calls":     {"giga.rpc.max_estimate_gas_calls": "-1"},
		"non-numeric estimate gas calls":  {"giga.rpc.max_estimate_gas_calls": "several"},
		"non-boolean state sync":          {"giga.state_sync.enabled": "maybe"},
	} {
		_, err := gigaconfig.ReadConfig(opts)
		require.Error(t, err, name)
	}
}

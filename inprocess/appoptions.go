//go:build inprocess

package inprocess

import (
	"time"

	"github.com/sei-protocol/sei-chain/app"
)

// appOptions is the per-node servertypes.AppOptions the harness injects into
// app.New. It enables EVM HTTP/WS on distinct per-node ports (the EVM-enable
// injection invariant — app.TestAppOpts disables them to dodge a fixed-port
// clash), sets the per-run chain-id, and disables the EVM stats tracker. The
// SeiDB flags are pinned explicitly rather than delegated to app.TestAppOpts:
// delegating would also adopt its giga-OFF default, flipping the in-process app
// off the production Giga execution engine (which an unset giga flag selects).
// Unknown keys return nil (the servertypes.AppOptions "unset, use default"
// contract).
type appOptions struct {
	chainID  string
	httpPort int
	wsPort   int
}

func (o appOptions) Get(key string) interface{} {
	switch key {
	case "chain-id":
		return o.chainID
	case "evm.http_enabled":
		return true
	case "evm.http_port":
		return o.httpPort
	case "evm.ws_enabled":
		return true
	case "evm.ws_port":
		return o.wsPort
	case "evm.rpc_stats_interval":
		// Disable the stats tracker. A positive interval (the unset default is 10s)
		// makes each EVM server spawn a reporter goroutine on a package-global that
		// EVMServer.Stop never cancels and that's reassigned per node — so N nodes
		// would orphan N-1. 0 skips it, keeping teardown deterministic.
		return time.Duration(0)
	case app.FlagSCEnable:
		return true
	case app.FlagSCSnapshotInterval:
		return uint32(0)
	case app.FlagSSEnable:
		return true
	case app.FlagSSBackend:
		return "pebbledb"
	case "evm.enabled_legacy_sei_apis":
		// The harness default gates all but 3 legacy sei_* methods; the rpc_io
		// conformance suite exercises the filter/log/tx family docker enables.
		return dockerLegacySeiApis
	}
	return nil
}

// dockerLegacySeiApis mirrors docker/localnode's enabled_legacy_sei_apis — every
// sei_/sei2_ method except sei_sign (which the suite asserts stays disabled).
var dockerLegacySeiApis = []string{
	"sei_associate", "sei_getBlockByHash", "sei_getBlockByHashExcludeTraceFail",
	"sei_getBlockByNumber", "sei_getBlockByNumberExcludeTraceFail", "sei_getBlockReceipts",
	"sei_getBlockTransactionCountByHash", "sei_getBlockTransactionCountByNumber",
	"sei_getCosmosTx", "sei_getEVMAddress", "sei_getEvmTx", "sei_getFilterChanges",
	"sei_getFilterLogs", "sei_getLogs", "sei_getSeiAddress",
	"sei_getTransactionByBlockHashAndIndex", "sei_getTransactionByBlockNumberAndIndex",
	"sei_getTransactionByHash", "sei_getTransactionCount", "sei_getTransactionErrorByHash",
	"sei_getTransactionReceipt", "sei_getTransactionReceiptExcludeTraceFail", "sei_getVMError",
	"sei_newBlockFilter", "sei_newFilter", "sei_uninstallFilter",
	"sei2_getBlockByHash", "sei2_getBlockByHashExcludeTraceFail", "sei2_getBlockByNumber",
	"sei2_getBlockByNumberExcludeTraceFail", "sei2_getBlockReceipts",
	"sei2_getBlockTransactionCountByHash", "sei2_getBlockTransactionCountByNumber",
}

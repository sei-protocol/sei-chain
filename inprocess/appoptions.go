//go:build inprocess

package inprocess

import (
	"time"

	"github.com/sei-protocol/sei-chain/app"
)

// appOptions is the per-node servertypes.AppOptions the harness injects into
// app.New. app.TestAppOpts hard-disables the EVM HTTP/WS listeners to avoid port
// clashes in single-app tests; the harness needs the opposite — EVM enabled on
// distinct per-node ports (the EVM-enable injection invariant) — plus the chain-id the sei-chain helpers
// hardcode. Unknown keys return nil, matching servertypes.AppOptions semantics
// (callers treat a nil as "unset, use the default").
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
	}
	return nil
}

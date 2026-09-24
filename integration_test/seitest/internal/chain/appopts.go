package chain

import (
	"github.com/sei-protocol/sei-chain/app"
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
)

// adminEnabledKey is the AppOptions key admin.ReadConfig reads to decide whether
// the admin gRPC server starts. It is duplicated here because the admin package
// keeps its flag names unexported; assertEVMServingOff reads the resolved
// admin.Config rather than this key, so a rename in admin degrades to that
// package's own default (off) instead of silently enabling the server.
const adminEnabledKey = "admin_server.admin_enabled"

// appOptions is the servertypes.AppOptions the engine injects into app.New. It
// pins every listener off and pins the SeiDB flags; unknown keys return nil,
// the servertypes.AppOptions "unset, use the default" contract.
//
// The Giga flags are deliberately absent so an unset flag selects the
// production execution engine. app.TestAppOpts carries a giga-OFF default,
// which is why the SeiDB flags are pinned here instead of delegated to it.
type appOptions struct {
	chainID string
}

var _ servertypes.AppOptions = appOptions{}

func (o appOptions) Get(key string) interface{} {
	switch key {
	case "chain-id":
		return o.chainID

	// EVM-serving-off: with no EVM listener, app.RegisterLocalServices constructs
	// no HTTP/WS server and reaches none of the evmrpc process-global sync.Once
	// singletons, which is what lets many networks live in one binary. Asserted
	// at the choke point by assertEVMServingOff.
	case "evm.http_enabled", "evm.ws_enabled":
		return false
	case adminEnabledKey:
		return false

	case app.FlagSCEnable:
		return true
	case app.FlagSCSnapshotInterval:
		// 0 disables snapshot creation, whose background goroutines are irrelevant
		// to a test network.
		return uint32(0)
	case app.FlagSCHashLoggerEnable:
		// The hash logger runs writer/control goroutines joined only by Store.Close
		// that rotate files under the home dir. Off keeps teardown ordering simple.
		return false
	case app.FlagSSEnable:
		return true
	case app.FlagSSBackend:
		return "pebbledb"
	}
	return nil
}

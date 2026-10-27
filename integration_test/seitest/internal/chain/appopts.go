package chain

import (
	"github.com/sei-protocol/sei-chain/app"
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
)

const adminEnabledKey = "admin_server.admin_enabled"

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

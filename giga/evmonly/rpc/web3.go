package rpc

import (
	"runtime"

	"github.com/sei-protocol/sei-chain/sei-cosmos/version"
)

type web3API struct{}

// ClientVersion returns the node's client identifier, Sei/<seid version>/<os>-<arch>/<go version>.
// The version is "unknown" when the binary was built without one.
func (*web3API) ClientVersion() string {
	v := version.Version
	if v == "" {
		v = "unknown"
	}
	return "Sei/" + v + "/" + runtime.GOOS + "-" + runtime.GOARCH + "/" + runtime.Version()
}

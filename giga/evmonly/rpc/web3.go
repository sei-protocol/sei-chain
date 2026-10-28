package rpc

import "runtime"

type web3API struct{}

// ClientVersion returns the client identifier the regular EVM RPC reports:
// Geth, the platform, and the Go version.
func (*web3API) ClientVersion() string {
	return "Geth/" + runtime.GOOS + "-" + runtime.GOARCH + "/" + runtime.Version()
}

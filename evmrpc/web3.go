package evmrpc

import "runtime"

type Web3API struct{}

func (w *Web3API) ClientVersion() string {
	name := "Geth" // Sei EVM is backed by go-ethereum
	name += "/" + runtime.GOOS + "-" + runtime.GOARCH
	name += "/" + runtime.Version()
	return name
}

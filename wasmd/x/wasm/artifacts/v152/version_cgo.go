//go:build cgo

package v152

import (
	"github.com/sei-protocol/sei-chain/wasmd/x/wasm/artifacts/v152/api"
)

func libwasmvmVersionImpl() (string, error) {
	return api.LibwasmvmVersion()
}

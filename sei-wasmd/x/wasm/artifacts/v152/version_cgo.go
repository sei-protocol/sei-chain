//go:build cgo

package v152

import (
	"github.com/CosmWasm/wasmd/x/wasm/artifacts/v152/api"
)

func libwasmvmVersionImpl() (string, error) {
	return api.LibwasmvmVersion()
}

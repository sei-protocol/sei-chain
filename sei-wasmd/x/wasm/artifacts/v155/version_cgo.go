//go:build cgo

package v155

import (
	"github.com/CosmWasm/wasmd/x/wasm/artifacts/v155/api"
)

func libwasmvmVersionImpl() (string, error) {
	return api.LibwasmvmVersion()
}

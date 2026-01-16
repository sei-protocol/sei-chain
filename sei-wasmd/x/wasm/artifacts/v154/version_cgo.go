//go:build cgo

package v154

import (
	"github.com/CosmWasm/wasmd/x/wasm/artifacts/v154/api"
)

func libwasmvmVersionImpl() (string, error) {
	return api.LibwasmvmVersion()
}

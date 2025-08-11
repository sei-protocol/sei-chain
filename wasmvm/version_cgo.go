//go:build cgo

package cosmwasm

import (
	"github.com/sei-protocol/sei-chain/wasmvm/internal/api"
)

func libwasmvmVersionImpl() (string, error) {
	return api.LibwasmvmVersion()
}

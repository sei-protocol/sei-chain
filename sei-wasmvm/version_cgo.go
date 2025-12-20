//go:build cgo

package cosmwasm

import (
	"github.com/sei-protocol/sei-chain/sei-wasmvm/internal/api"
)

func libwasmvmVersionImpl() (string, error) {
	return api.LibwasmvmVersion()
}

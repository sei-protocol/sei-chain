package ibc

import (
	"embed"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

const IBCAddress = "0x0000000000000000000000000000000000001009"

const RetiredReason = "ibc precompile is retired; ibc transfers are disabled"

//go:embed abi.json
var currentABI embed.FS

// NewPrecompile keeps the IBC address registered but retires all of its
// methods because IBC transfers are permanently disabled.
func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	return newRetiredPrecompile(pcommon.MustGetABI(currentABI, "abi.json"), keepers), nil
}

func newRetiredPrecompile(a abi.ABI, keepers utils.Keepers) *pcommon.DynamicGasPrecompile {
	return pcommon.NewRetiredPrecompile(
		a,
		common.HexToAddress(IBCAddress),
		"ibc",
		keepers.EVMK(),
		RetiredReason,
	)
}

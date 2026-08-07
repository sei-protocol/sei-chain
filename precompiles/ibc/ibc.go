package ibc

import (
	"embed"

	"github.com/ethereum/go-ethereum/common"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

const (
	IBCAddress = "0x0000000000000000000000000000000000001009"
)

// RetiredReason is returned by every call to the IBC precompile. IBC inbound
// and outbound are disabled on chain and are not coming back, so the precompile
// can no longer reach a transfer keeper.
const RetiredReason = "ibc precompile is retired; ibc transfers are disabled"

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	return pcommon.NewRetiredPrecompile(
		pcommon.MustGetABI(f, "abi.json"),
		common.HexToAddress(IBCAddress),
		"ibc",
		keepers.EVMK(),
		RetiredReason,
	), nil
}

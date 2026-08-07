package v620

import (
	"embed"

	"github.com/ethereum/go-ethereum/common"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

const (
	IBCAddress = "0x0000000000000000000000000000000000001009"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

// NewPrecompile returns the retired IBC precompile for this upgrade tag. Every
// version reverts: the transfer keeper this precompile called is gone, so no
// tag can execute a transfer regardless of the height being replayed.
func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	return pcommon.NewRetiredPrecompile(
		pcommon.MustGetABI(f, "abi.json"),
		common.HexToAddress(IBCAddress),
		"ibc",
		keepers.EVMK(),
		"ibc precompile is retired; ibc transfers are disabled",
	), nil
}

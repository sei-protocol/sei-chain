package types

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// intrinsicGasRules are the forks Sei prices intrinsic gas under: Homestead, Istanbul and Shanghai.
var intrinsicGasRules = params.Rules{IsHomestead: true, IsIstanbul: true, IsShanghai: true}

// IntrinsicGas returns the intrinsic gas of an EVM transaction under Sei's rules. It fails when the
// transaction's value does not fit in 256 bits.
func IntrinsicGas(etx *ethtypes.Transaction) (uint64, error) {
	value, overflow := uint256.FromBig(etx.Value())
	if overflow {
		return 0, fmt.Errorf("value exceeds 256 bits: %d bits", etx.Value().BitLen())
	}
	return core.IntrinsicGas(etx.Data(), etx.AccessList(), etx.SetCodeAuthorizations(), common.Address{}, etx.To(), value, intrinsicGasRules)
}

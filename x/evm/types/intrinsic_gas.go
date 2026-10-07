package types

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// intrinsicGasRules are the forks Sei prices intrinsic gas under: Homestead, Istanbul and Shanghai.
var intrinsicGasRules = params.Rules{IsHomestead: true, IsIstanbul: true, IsShanghai: true}

// IntrinsicGas returns the intrinsic gas of an EVM transaction under Sei's rules.
func IntrinsicGas(etx *ethtypes.Transaction) (uint64, error) {
	value, _ := uint256.FromBig(etx.Value())
	return core.IntrinsicGas(etx.Data(), etx.AccessList(), etx.SetCodeAuthorizations(), common.Address{}, etx.To(), value, intrinsicGasRules)
}

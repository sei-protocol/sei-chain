package types

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// intrinsicGasRules are the rules Sei applies when computing a transaction's
// intrinsic gas: Homestead contract-creation pricing, EIP-2028 calldata
// pricing and EIP-3860 initcode word pricing. Later forks that re-price
// intrinsic gas (e.g. Amsterdam) are intentionally not enabled here.
var intrinsicGasRules = params.Rules{IsHomestead: true, IsIstanbul: true, IsShanghai: true}

// IntrinsicGas returns the intrinsic gas of an EVM transaction under Sei's rules.
func IntrinsicGas(etx *ethtypes.Transaction) (uint64, error) {
	value, _ := uint256.FromBig(etx.Value())
	return core.IntrinsicGas(etx.Data(), etx.AccessList(), etx.SetCodeAuthorizations(), common.Address{}, etx.To(), value, intrinsicGasRules)
}

package evmonlyapp

import (
	"math/big"

	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

// evmOnlyBaseFeeLag is how many blocks after a block its gas use sets the base fee of.
const evmOnlyBaseFeeLag = 2

// evmOnlyGenesisEVMParams are the base fee params gov's first EndBlock writes. Base fees are set by
// them while no params are written: on a chain without gov, and in gov's first block.
var evmOnlyGenesisEVMParams = chainparams.EVM{
	MinFeePerGas:             new(big.Int),
	TargetGasUsedPerSecond:   2_000_000_000,
	BaseFeeChangeDenominator: 144,
}

// evmOnlyBaseFeeParams returns the base fee rules of the params state holds, or of
// evmOnlyGenesisEVMParams when it holds none.
func evmOnlyBaseFeeParams(state chainparams.StorageReader) evmonly.BaseFeeParams {
	if !chainparams.HasEVM(state) {
		return baseFeeParams(evmOnlyGenesisEVMParams)
	}
	return baseFeeParams(chainparams.ReadEVM(state))
}

// baseFeeParams returns the base fee rules p sets.
func baseFeeParams(p chainparams.EVM) evmonly.BaseFeeParams {
	var floor uint256.Int
	floor.SetFromBig(p.MinFeePerGas)
	return evmonly.BaseFeeParams{
		Floor:              floor,
		TargetGasPerSecond: p.TargetGasUsedPerSecond,
		Denominator:        p.BaseFeeChangeDenominator,
	}
}

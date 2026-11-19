package evmonlyapp

import (
	"math/big"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

// evmOnlyBaseFeeLag is how many blocks after a block its gas use sets the base fee of.
const evmOnlyBaseFeeLag = 2

// evmOnlyGenesisBaseFee is the base fee, in wei per gas, a chain's first blocks run at. It is not a
// floor: the base fee update moves the fee from it either way.
const evmOnlyGenesisBaseFee = 1_000_000_000

// evmOnlyGenesisEVMParams are the base fee params gov's first EndBlock writes: a block using its
// whole gas limit raises the base fee by 0.6%, and an empty one lowers it by 0.6%. Base fees are
// set by them while no params are written: on a chain without gov, and in gov's first block.
var evmOnlyGenesisEVMParams = chainparams.EVM{
	MaxDynamicBaseFeeUpwardAdjustment:   big.NewInt(6_000_000_000_000_000),
	MaxDynamicBaseFeeDownwardAdjustment: big.NewInt(6_000_000_000_000_000),
}

// evmOnlyBaseFeeParams returns the base fee rules of the params state holds, or of
// evmOnlyGenesisEVMParams when it holds none.
func evmOnlyBaseFeeParams(state chainparams.StorageReader) evmonly.BaseFeeParams {
	if !chainparams.HasEVM(state) {
		return baseFeeParams(evmOnlyGenesisEVMParams)
	}
	return baseFeeParams(chainparams.ReadEVM(state))
}

// baseFeeParams returns the base fee rules p sets. A Dec and a base fee adjustment share their
// scale.
func baseFeeParams(p chainparams.EVM) evmonly.BaseFeeParams {
	var params evmonly.BaseFeeParams
	params.MaxIncrease.SetFromBig(p.MaxDynamicBaseFeeUpwardAdjustment)
	params.MaxDecrease.SetFromBig(p.MaxDynamicBaseFeeDownwardAdjustment)
	return params
}

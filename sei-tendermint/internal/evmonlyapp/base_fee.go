package evmonlyapp

import (
	"math/big"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

// evmOnlyGenesisMinFeePerGas is the genesis KeyMinFeePerGas, in wei per gas.
const evmOnlyGenesisMinFeePerGas = 1_000_000_000

// evmOnlyGenesisEVMParams are the base fee params gov's first EndBlock writes: a block using its
// whole gas limit raises the base fee two blocks later by 0.6%, and an empty one lowers it by 0.6%,
// never below 1 gwei. Base fees are set by them while no params are written: on a chain without
// gov, and in gov's first block.
var evmOnlyGenesisEVMParams = chainparams.EVM{
	MaxDynamicBaseFeeUpwardAdjustment:   big.NewInt(6_000_000_000_000_000),
	MaxDynamicBaseFeeDownwardAdjustment: big.NewInt(6_000_000_000_000_000),
	MinFeePerGas:                        new(big.Int).Mul(big.NewInt(evmOnlyGenesisMinFeePerGas), evmonly.BaseFeeAdjustmentOne.ToBig()),
	BaseFeeLag:                          2,
}

// baseFeeRules are the rules that schedule base fees.
type baseFeeRules struct {
	// step moves the base fee from one scheduled block to the next.
	step evmonly.BaseFeeParams
	// lag is how many blocks after a block its gas use sets the base fee of.
	lag int
}

// evmOnlyBaseFeeRules returns the base fee rules of the params state holds, or of
// evmOnlyGenesisEVMParams when it holds none.
func evmOnlyBaseFeeRules(state chainparams.StorageReader) baseFeeRules {
	if !chainparams.HasEVM(state) {
		return baseFeeRulesOf(evmOnlyGenesisEVMParams)
	}
	return baseFeeRulesOf(chainparams.ReadEVM(state))
}

// baseFeeRulesOf returns the base fee rules p sets. A Dec and a base fee adjustment share their
// scale, so the minimum fee per gas in wei is the Dec's whole part.
func baseFeeRulesOf(p chainparams.EVM) baseFeeRules {
	var rules baseFeeRules
	rules.step.MaxIncrease.SetFromBig(p.MaxDynamicBaseFeeUpwardAdjustment)
	rules.step.MaxDecrease.SetFromBig(p.MaxDynamicBaseFeeDownwardAdjustment)
	rules.step.MinBaseFee.SetFromBig(new(big.Int).Quo(p.MinFeePerGas, evmonly.BaseFeeAdjustmentOne.ToBig()))
	rules.lag = int(p.BaseFeeLag)
	return rules
}

// genesisBaseFees returns the base fees of the cursor InitChain writes: the minimum base fee of
// evmOnlyGenesisEVMParams, for the block before the first and each of the lag blocks after it.
func genesisBaseFees() evmOnlyBaseFees {
	rules := baseFeeRulesOf(evmOnlyGenesisEVMParams)
	fees := make(evmOnlyBaseFees, rules.lag+1)
	for i := range fees {
		fees[i] = rules.step.MinBaseFee
	}
	return fees
}

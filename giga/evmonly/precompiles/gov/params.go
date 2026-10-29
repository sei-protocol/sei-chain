package gov

import (
	"math"
	"math/big"
	"time"

	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

// Params are gov's params as gov uses them. Periods are nanoseconds, deposits
// are wei, and ratios are 1e18-scaled decimals.
type Params struct {
	VotingPeriod          uint64   `json:"voting_period"`
	ExpeditedVotingPeriod uint64   `json:"expedited_voting_period"`
	MinDeposit            *big.Int `json:"min_deposit"`
	MaxDepositPeriod      uint64   `json:"max_deposit_period"`
	MinExpeditedDeposit   *big.Int `json:"min_expedited_deposit"`
	Quorum                uint64   `json:"quorum"`
	Threshold             uint64   `json:"threshold"`
	VetoThreshold         uint64   `json:"veto_threshold"`
	ExpeditedQuorum       uint64   `json:"expedited_quorum"`
	ExpeditedThreshold    uint64   `json:"expedited_threshold"`
}

// DefaultParams returns x/gov's default params.
func DefaultParams() Params {
	return Params{
		VotingPeriod:          uint64(48 * time.Hour),
		ExpeditedVotingPeriod: uint64(24 * time.Hour),
		MinDeposit:            new(big.Int).Mul(big.NewInt(10_000_000), weiPerUsei),
		MaxDepositPeriod:      uint64(48 * time.Hour),
		MinExpeditedDeposit:   new(big.Int).Mul(big.NewInt(20_000_000), weiPerUsei),
		Quorum:                334_000_000_000_000_000,
		Threshold:             500_000_000_000_000_000,
		VetoThreshold:         334_000_000_000_000_000,
		ExpeditedQuorum:       667_000_000_000_000_000,
		ExpeditedThreshold:    667_000_000_000_000_000,
	}
}

// Validate checks p as the params precompile checks gov's params.
func (p Params) Validate() error {
	return p.stored().Validate()
}

// stored returns p in ParamsStorage's units.
func (p Params) stored() chainparams.Gov {
	return chainparams.Gov{
		MaxDepositPeriod:      p.MaxDepositPeriod,
		VotingPeriod:          p.VotingPeriod,
		ExpeditedVotingPeriod: p.ExpeditedVotingPeriod,
		MinDeposit:            p.MinDeposit,
		MinExpeditedDeposit:   p.MinExpeditedDeposit,
		Quorum:                new(big.Int).SetUint64(p.Quorum),
		Threshold:             new(big.Int).SetUint64(p.Threshold),
		VetoThreshold:         new(big.Int).SetUint64(p.VetoThreshold),
		ExpeditedQuorum:       new(big.Int).SetUint64(p.ExpeditedQuorum),
		ExpeditedThreshold:    new(big.Int).SetUint64(p.ExpeditedThreshold),
	}
}

// paramsFrom returns stored gov params in gov's units.
func paramsFrom(g chainparams.Gov) Params {
	return Params{
		VotingPeriod:          g.VotingPeriod,
		ExpeditedVotingPeriod: g.ExpeditedVotingPeriod,
		MinDeposit:            g.MinDeposit,
		MaxDepositPeriod:      g.MaxDepositPeriod,
		MinExpeditedDeposit:   g.MinExpeditedDeposit,
		Quorum:                decUint64(g.Quorum),
		Threshold:             decUint64(g.Threshold),
		VetoThreshold:         decUint64(g.VetoThreshold),
		ExpeditedQuorum:       decUint64(g.ExpeditedQuorum),
		ExpeditedThreshold:    decUint64(g.ExpeditedThreshold),
	}
}

// decUint64 returns a stored ratio, which validation bounds by 1e18.
func decUint64(v *big.Int) uint64 {
	if !v.IsUint64() {
		return math.MaxUint64
	}
	return v.Uint64()
}

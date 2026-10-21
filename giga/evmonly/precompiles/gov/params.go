package gov

import (
	"errors"
	"fmt"
	"math/big"
)

// maxPeriod bounds every period, in seconds, so an end time cannot overflow.
const maxPeriod = 1 << 40

// DefaultParams returns x/gov's default params.
func DefaultParams() Params {
	return Params{
		VotingPeriod:          2 * 24 * 60 * 60,
		ExpeditedVotingPeriod: 24 * 60 * 60,
		MinDeposit:            new(big.Int).Mul(big.NewInt(10_000_000), weiPerUsei),
		MaxDepositPeriod:      2 * 24 * 60 * 60,
		MinExpeditedDeposit:   new(big.Int).Mul(big.NewInt(20_000_000), weiPerUsei),
		Quorum:                334_000_000_000_000_000,
		Threshold:             500_000_000_000_000_000,
		VetoThreshold:         334_000_000_000_000_000,
		ExpeditedQuorum:       667_000_000_000_000_000,
		ExpeditedThreshold:    667_000_000_000_000_000,
	}
}

// Validate checks p as x/gov checks its params.
func (p Params) Validate() error {
	if p.VotingPeriod == 0 || p.VotingPeriod > maxPeriod {
		return fmt.Errorf("voting_period must be in 1..%d seconds", uint64(maxPeriod))
	}
	if p.ExpeditedVotingPeriod == 0 || p.ExpeditedVotingPeriod >= p.VotingPeriod {
		return errors.New("expedited_voting_period must be positive and below voting_period")
	}
	if p.MaxDepositPeriod == 0 || p.MaxDepositPeriod > maxPeriod {
		return fmt.Errorf("max_deposit_period must be in 1..%d seconds", uint64(maxPeriod))
	}
	for name, d := range map[string]*big.Int{"min_deposit": p.MinDeposit, "min_expedited_deposit": p.MinExpeditedDeposit} {
		if d == nil || d.Sign() < 0 || new(big.Int).Mod(d, weiPerUsei).Sign() != 0 {
			return fmt.Errorf("%s must be a non-negative whole number of usei, in wei", name)
		}
	}
	if p.MinExpeditedDeposit.Cmp(p.MinDeposit) < 0 {
		return errors.New("min_expedited_deposit must not be below min_deposit")
	}
	for name, r := range map[string]uint64{"quorum": p.Quorum, "expedited_quorum": p.ExpeditedQuorum} {
		if r > decOne {
			return fmt.Errorf("%s must be at most 1e18", name)
		}
	}
	for name, r := range map[string]uint64{"threshold": p.Threshold, "veto_threshold": p.VetoThreshold, "expedited_threshold": p.ExpeditedThreshold} {
		if r == 0 || r > decOne {
			return fmt.Errorf("%s must be in 1..1e18", name)
		}
	}
	return nil
}

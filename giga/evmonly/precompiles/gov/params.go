package gov

import (
	"errors"
	"fmt"
	"math/big"
	"time"
)

// Bounds on every period, in nanoseconds.
const (
	minPeriod = uint64(time.Second)
	maxPeriod = 1 << 62 // a block time plus any period fits in 64 bits
)

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

// Validate checks p as x/gov checks its params, and also requires periods of
// 1s to 2^62ns and positive minimum deposits, which x/gov does not.
func (p Params) Validate() error {
	for _, d := range []struct {
		name   string
		period uint64
	}{{"voting_period", p.VotingPeriod}, {"expedited_voting_period", p.ExpeditedVotingPeriod}, {"max_deposit_period", p.MaxDepositPeriod}} {
		if d.period < minPeriod || d.period > maxPeriod {
			return fmt.Errorf("%s must be in %d..%d nanoseconds", d.name, minPeriod, uint64(maxPeriod))
		}
	}
	if p.ExpeditedVotingPeriod >= p.VotingPeriod {
		return errors.New("expedited_voting_period must be below voting_period")
	}
	// The depositor cap relies on a positive minimum: a zero one makes
	// depositorShare zero, so 200 dust deposits could fill every slot.
	for name, d := range map[string]*big.Int{"min_deposit": p.MinDeposit, "min_expedited_deposit": p.MinExpeditedDeposit} {
		if d == nil || d.Sign() <= 0 || new(big.Int).Mod(d, weiPerUsei).Sign() != 0 {
			return fmt.Errorf("%s must be a positive whole number of usei, in wei", name)
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

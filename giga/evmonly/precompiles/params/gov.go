package params

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Gov's flat keys.
const (
	keyMinDeposit            = "depositparams.min_deposit"
	keyMaxDepositPeriod      = "depositparams.max_deposit_period"
	keyMinExpeditedDeposit   = "depositparams.min_expedited_deposit"
	keyVotingPeriod          = "votingparams.voting_period"
	keyExpeditedVotingPeriod = "votingparams.expedited_voting_period"
	keyQuorum                = "tallyparams.quorum"
	keyThreshold             = "tallyparams.threshold"
	keyVetoThreshold         = "tallyparams.veto_threshold"
	keyExpeditedQuorum       = "tallyparams.expedited_quorum"
	keyExpeditedThreshold    = "tallyparams.expedited_threshold"
)

// weiPerUsei is the wei in one usei, the smallest unit a deposit may hold.
var weiPerUsei = big.NewInt(1_000_000_000_000)

var govSubspace = subspace{
	name: "gov",
	slot: slotGov,
	params: []param{
		{key: keyMinDeposit, kind: coinsKind, at: govMinDeposit},
		{key: keyMaxDepositPeriod, kind: durationKind, at: govMaxDepositPeriod},
		{key: keyMinExpeditedDeposit, kind: coinsKind, at: govMinExpeditedDeposit},
		{key: keyVotingPeriod, kind: durationKind, at: govVotingPeriod, least: new(big.Int).SetUint64(MinChangedVotingPeriod)},
		{key: keyExpeditedVotingPeriod, kind: durationKind, at: govExpeditedVotingPeriod},
		{key: keyQuorum, kind: decKind, at: govQuorum},
		{key: keyThreshold, kind: decKind, at: govThreshold},
		{key: keyVetoThreshold, kind: decKind, at: govVetoThreshold},
		{key: keyExpeditedQuorum, kind: decKind, at: govExpeditedQuorum},
		{key: keyExpeditedThreshold, kind: decKind, at: govExpeditedThreshold},
	},
	check: func(v values) error { return govFrom(v).Validate() },
}

// Gov is ParamsStorage.Gov. Periods are nanoseconds, deposits are wei, and
// ratios are 1e18-scaled decimals.
type Gov struct {
	MaxDepositPeriod      uint64
	VotingPeriod          uint64
	ExpeditedVotingPeriod uint64
	MinDeposit            *big.Int
	MinExpeditedDeposit   *big.Int
	Quorum                *big.Int
	Threshold             *big.Int
	VetoThreshold         *big.Int
	ExpeditedQuorum       *big.Int
	ExpeditedThreshold    *big.Int
}

// ReadGov returns gov's params.
func ReadGov(state precompiles.State) Gov {
	return govFrom(govSubspace.read(newStore(state)))
}

// govVotingPeriodParam is the param HasGov reads.
var govVotingPeriodParam = govSubspace.mustParam(keyVotingPeriod)

// HasGov reports whether gov's params have been written.
func HasGov(state precompiles.State) bool {
	return govSubspace.get(newStore(state), govVotingPeriodParam).Sign() != 0
}

// WriteGov writes g as gov's params, for genesis, if g is valid.
func WriteGov(state precompiles.State, g Gov) error {
	v := g.values()
	if err := govSubspace.validate(v); err != nil {
		return err
	}
	s := newStore(state)
	for _, p := range govSubspace.params {
		govSubspace.set(s, p, v[p.key])
	}
	return nil
}

func govFrom(v values) Gov {
	return Gov{
		MaxDepositPeriod:      v[keyMaxDepositPeriod].Uint64(),
		VotingPeriod:          v[keyVotingPeriod].Uint64(),
		ExpeditedVotingPeriod: v[keyExpeditedVotingPeriod].Uint64(),
		MinDeposit:            v[keyMinDeposit],
		MinExpeditedDeposit:   v[keyMinExpeditedDeposit],
		Quorum:                v[keyQuorum],
		Threshold:             v[keyThreshold],
		VetoThreshold:         v[keyVetoThreshold],
		ExpeditedQuorum:       v[keyExpeditedQuorum],
		ExpeditedThreshold:    v[keyExpeditedThreshold],
	}
}

func (g Gov) values() values {
	return values{
		keyMaxDepositPeriod:      new(big.Int).SetUint64(g.MaxDepositPeriod),
		keyVotingPeriod:          new(big.Int).SetUint64(g.VotingPeriod),
		keyExpeditedVotingPeriod: new(big.Int).SetUint64(g.ExpeditedVotingPeriod),
		keyMinDeposit:            g.MinDeposit,
		keyMinExpeditedDeposit:   g.MinExpeditedDeposit,
		keyQuorum:                g.Quorum,
		keyThreshold:             g.Threshold,
		keyVetoThreshold:         g.VetoThreshold,
		keyExpeditedQuorum:       g.ExpeditedQuorum,
		keyExpeditedThreshold:    g.ExpeditedThreshold,
	}
}

// named is a value and the param name its errors use.
type named struct {
	name string
	v    *big.Int
}

// Bounds on every gov period, in nanoseconds.
const (
	MinPeriod uint64 = uint64(time.Second)
	MaxPeriod uint64 = 1 << 62 // a block time in Unix nanoseconds plus any period fits in 64 bits
)

// Bounds on the gov params that, set past them, could leave gov unable to pass a
// later proposal. MinChangedVotingPeriod bounds only param changes, not genesis.
const (
	MaxVotingPeriod        uint64 = uint64(30 * 24 * time.Hour)
	MinChangedVotingPeriod uint64 = uint64(5 * time.Minute)
)

var (
	maxMinDeposit = new(big.Int).Mul(big.NewInt(1_000_000_000_000), weiPerUsei) // 1,000,000 SEI
	maxQuorum     = big.NewInt(800_000_000_000_000_000)                         // 4/5
	maxThreshold  = big.NewInt(800_000_000_000_000_000)                         // 4/5
)

// Validate checks g as x/gov checks its params, and also requires periods of
// MinPeriod to MaxPeriod, positive minimum deposits of whole usei, and
// voting_period, min_deposit, quorum and threshold at most MaxVotingPeriod,
// 1,000,000 SEI, 4/5 and 4/5.
func (g Gov) Validate() error {
	for _, p := range []struct {
		name string
		v    uint64
	}{{"voting_period", g.VotingPeriod}, {"expedited_voting_period", g.ExpeditedVotingPeriod}, {"max_deposit_period", g.MaxDepositPeriod}} {
		if p.v < MinPeriod || p.v > MaxPeriod {
			return fmt.Errorf("%s must be in %d..%d nanoseconds", p.name, MinPeriod, MaxPeriod)
		}
	}
	if g.VotingPeriod > MaxVotingPeriod {
		return fmt.Errorf("voting_period must be at most %d nanoseconds", MaxVotingPeriod)
	}
	if g.ExpeditedVotingPeriod >= g.VotingPeriod {
		return errors.New("expedited_voting_period must be below voting_period")
	}
	for _, d := range []named{{"min_deposit", g.MinDeposit}, {"min_expedited_deposit", g.MinExpeditedDeposit}} {
		if d.v == nil || d.v.Sign() <= 0 || new(big.Int).Mod(d.v, weiPerUsei).Sign() != 0 {
			return fmt.Errorf("%s must be a positive whole number of usei, in wei", d.name)
		}
	}
	if g.MinDeposit.Cmp(maxMinDeposit) > 0 {
		return fmt.Errorf("min_deposit must be at most %s wei", maxMinDeposit)
	}
	if g.MinExpeditedDeposit.Cmp(g.MinDeposit) <= 0 {
		return errors.New("min_expedited_deposit must be above min_deposit")
	}
	for _, r := range []named{{"quorum", g.Quorum}, {"expedited_quorum", g.ExpeditedQuorum}} {
		if r.v == nil || r.v.Sign() < 0 || r.v.Cmp(decOne) > 0 {
			return fmt.Errorf("%s must be in 0..1", r.name)
		}
	}
	for _, r := range []named{{"threshold", g.Threshold}, {"veto_threshold", g.VetoThreshold}, {"expedited_threshold", g.ExpeditedThreshold}} {
		if r.v == nil || r.v.Sign() <= 0 || r.v.Cmp(decOne) > 0 {
			return fmt.Errorf("%s must be above 0 and at most 1", r.name)
		}
	}
	if g.Quorum.Cmp(maxQuorum) > 0 {
		return fmt.Errorf("quorum must be at most %s", maxQuorum)
	}
	if g.Threshold.Cmp(maxThreshold) > 0 {
		return fmt.Errorf("threshold must be at most %s", maxThreshold)
	}
	if g.ExpeditedQuorum.Cmp(g.Quorum) <= 0 {
		return errors.New("expedited_quorum must be above quorum")
	}
	if g.ExpeditedThreshold.Cmp(g.Threshold) <= 0 {
		return errors.New("expedited_threshold must be above threshold")
	}
	return nil
}

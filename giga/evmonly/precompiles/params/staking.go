package params

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// Staking's flat keys.
const (
	keyUnbondingTime                      = "UnbondingTime"
	keyMaxValidators                      = "MaxValidators"
	keyMaxEntries                         = "MaxEntries"
	keyMaxVotingPowerRatio                = "MaxVotingPower"
	keyMaxVotingPowerEnforcementThreshold = "MaxVotingPowerEnforcementThreshold"
)

var stakingSubspace = subspace{
	name: "staking",
	slot: slotStaking,
	params: []param{
		{key: keyUnbondingTime, kind: durationKind, at: stakingUnbondingTime},
		{key: keyMaxValidators, kind: uint32Kind, at: stakingMaxValidators},
		{key: keyMaxEntries, kind: uint32Kind, at: stakingMaxEntries},
		{key: keyMaxVotingPowerRatio, kind: decKind, at: stakingMaxVotingPowerRatio},
		{key: keyMaxVotingPowerEnforcementThreshold, kind: intKind, at: stakingMaxVotingPowerEnforcementThreshold},
	},
	check: func(v values) error { return stakingFrom(v).Validate() },
}

// Staking is ParamsStorage.Staking. The unbonding time is nanoseconds, the max
// voting power ratio is a 1e18-scaled decimal, and its enforcement threshold is
// consensus power.
type Staking struct {
	UnbondingTime                      uint64
	MaxValidators                      uint32
	MaxEntries                         uint32
	MaxVotingPowerRatio                *big.Int
	MaxVotingPowerEnforcementThreshold *big.Int
}

// ReadStaking returns staking's params.
func ReadStaking(state precompiles.State) Staking {
	return stakingFrom(stakingSubspace.read(newStore(state)))
}

// stakingUnbondingTimeParam is the param HasStaking reads.
var stakingUnbondingTimeParam = stakingSubspace.mustParam(keyUnbondingTime)

// HasStaking reports whether staking's params have been written.
func HasStaking(state precompiles.State) bool {
	return stakingSubspace.get(newStore(state), stakingUnbondingTimeParam).Sign() != 0
}

var errStakingWritten = errors.New("staking's params are already written")

// WriteStaking writes s as staking's params, for genesis, if s is valid and
// staking's params have not been written.
func WriteStaking(state precompiles.State, s Staking) error {
	if HasStaking(state) {
		return errStakingWritten
	}
	v := s.values()
	if err := stakingSubspace.validate(v); err != nil {
		return err
	}
	store := newStore(state)
	for _, p := range stakingSubspace.params {
		stakingSubspace.set(store, p, v[p.key])
	}
	return nil
}

func stakingFrom(v values) Staking {
	return Staking{
		UnbondingTime:                      v[keyUnbondingTime].Uint64(),
		MaxValidators:                      uint32(v[keyMaxValidators].Uint64()), //nolint:gosec // validate bounds it to 32 bits
		MaxEntries:                         uint32(v[keyMaxEntries].Uint64()),    //nolint:gosec // validate bounds it to 32 bits
		MaxVotingPowerRatio:                v[keyMaxVotingPowerRatio],
		MaxVotingPowerEnforcementThreshold: v[keyMaxVotingPowerEnforcementThreshold],
	}
}

func (s Staking) values() values {
	return values{
		keyUnbondingTime:                      new(big.Int).SetUint64(s.UnbondingTime),
		keyMaxValidators:                      new(big.Int).SetUint64(uint64(s.MaxValidators)),
		keyMaxEntries:                         new(big.Int).SetUint64(uint64(s.MaxEntries)),
		keyMaxVotingPowerRatio:                s.MaxVotingPowerRatio,
		keyMaxVotingPowerEnforcementThreshold: s.MaxVotingPowerEnforcementThreshold,
	}
}

// MaxBondedValidators is the most validators MaxValidators may bond: the most
// members an autobahn committee holds.
const MaxBondedValidators uint32 = atypes.MaxValidators

// Validate checks s as x/staking checks its params, and also requires an
// unbonding time of MinPeriod to MaxPeriod and at most MaxBondedValidators
// bonded validators.
func (s Staking) Validate() error {
	if s.UnbondingTime < MinPeriod || s.UnbondingTime > MaxPeriod {
		return fmt.Errorf("unbonding_time must be in %d..%d nanoseconds", MinPeriod, MaxPeriod)
	}
	if s.MaxValidators == 0 || s.MaxValidators > MaxBondedValidators {
		return fmt.Errorf("max_validators must be in 1..%d", MaxBondedValidators)
	}
	if s.MaxEntries == 0 {
		return errors.New("max_entries must be positive")
	}
	if r := s.MaxVotingPowerRatio; r == nil || r.Sign() <= 0 || r.Cmp(decOne) > 0 {
		return errors.New("max_voting_power_ratio must be above 0 and at most 1")
	}
	if s.MaxVotingPowerEnforcementThreshold == nil || s.MaxVotingPowerEnforcementThreshold.Sign() < 0 {
		return errors.New("max_voting_power_enforcement_threshold must not be negative")
	}
	return nil
}

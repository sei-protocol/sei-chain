package staking

import (
	"fmt"
	"math/big"
	"math/rand"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/dec"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

var (
	val4             = common.HexToAddress("0x0000000000000000000000000000000000000a04")
	carol            = common.HexToAddress("0x00000000000000000000000000000000000ca401")
	unknownValidator = common.HexToAddress("0x0000000000000000000000000000000000000a0f")
)

// parityParams bond three validators, unbond over thirty seconds, allow three
// entries, and cap a validator at half the bonded power.
var parityParams = chainparams.Staking{
	UnbondingTime:                      30 * second,
	MaxValidators:                      3,
	MaxEntries:                         3,
	MaxVotingPowerRatio:                new(big.Int).Div(dec.One(), big.NewInt(2)),
	MaxVotingPowerEnforcementThreshold: big.NewInt(1),
}

// newParityPair starts val1 to val4 self-delegating 30, 25, 20 and 15 SEI, with
// alice, bob and carol to delegate, then runs the first EndBlock, which bonds
// val1, val2 and val3.
func newParityPair(t *testing.T) *pair {
	return newPair(t, parityParams, []parityValidator{
		{val1, 30 * seiInUsei, seiInUsei},
		{val2, 25 * seiInUsei, seiInUsei},
		{val3, 20 * seiInUsei, seiInUsei},
		{val4, 15 * seiInUsei, seiInUsei},
	}, alice, bob, carol)
}

func (pr *pair) v2UnbondingEntries(delegator, op common.Address) int {
	ubd, _ := pr.v2.keeper.GetUnbondingDelegation(pr.v2.ctx, sdk.AccAddress(delegator[:]), sdk.ValAddress(op[:]))
	return len(ubd.Entries)
}

func (pr *pair) v2Status(op common.Address) stakingtypes.BondStatus {
	v, ok := pr.v2.validator(op)
	require.True(pr.t, ok, "%s exists", op)
	return v.Status
}

// TestDelegationMatchesV2 takes delegate and undelegate through each of
// x/staking's rules on the precompile and on x/staking, checking after every
// step that both end it alike, call the same hooks and hold the same state.
func TestDelegationMatchesV2(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(t *testing.T, pr *pair)
	}{
		{"undelegations from bonded and unbonded validators pay out when due", func(t *testing.T, pr *pair) {
			require.NoError(t, pr.delegate(alice, val1, 5*seiInUsei))
			require.NoError(t, pr.delegate(alice, val4, 2*seiInUsei+7))
			require.NoError(t, pr.undelegate(alice, val1, 2*seiInUsei))
			require.NoError(t, pr.undelegate(alice, val4, seiInUsei+3))
			pr.endBlock()
			require.NoError(t, pr.undelegate(alice, val1, seiInUsei))
			pr.wait(parityParams.UnbondingTime - second)
			pr.endBlock()
			require.Equal(t, 1, pr.v2UnbondingEntries(alice, val1), "the later entry waits")
			require.Zero(t, pr.v2UnbondingEntries(alice, val4))
			pr.endBlock()
			require.Zero(t, pr.v2UnbondingEntries(alice, val1))
		}},
		{"a full withdrawal removes the delegation", func(t *testing.T, pr *pair) {
			require.NoError(t, pr.delegate(alice, val2, 3*seiInUsei))
			require.NoError(t, pr.undelegate(alice, val2, 3*seiInUsei))
			_, found := pr.v2.keeper.GetDelegation(pr.v2.ctx, sdk.AccAddress(alice[:]), sdk.ValAddress(val2[:]))
			require.False(t, found)
		}},
		{"unbonding entries stop at the max entries", func(t *testing.T, pr *pair) {
			require.NoError(t, pr.delegate(alice, val1, 10*seiInUsei))
			for range parityParams.MaxEntries {
				require.NoError(t, pr.undelegate(alice, val1, seiInUsei))
			}
			require.ErrorIs(t, pr.undelegate(alice, val1, seiInUsei), stakingtypes.ErrMaxUnbondingDelegationEntries)
			pr.wait(parityParams.UnbondingTime)
			pr.endBlock()
			require.NoError(t, pr.undelegate(alice, val1, seiInUsei))
		}},
		{"the voting power ratio caps delegations", func(t *testing.T, pr *pair) {
			require.ErrorIs(t, pr.delegate(alice, val1, 20*seiInUsei), stakingtypes.ErrExceedMaxVotingPowerRatio, "50 of 95")
			require.NoError(t, pr.delegate(alice, val1, 10*seiInUsei), "40 of 85")
		}},
		{"a delegation is refused by each of its checks", func(t *testing.T, pr *pair) {
			require.ErrorIs(t, pr.delegate(alice, unknownValidator, seiInUsei), stakingtypes.ErrNoValidatorFound)
			require.ErrorIs(t, pr.undelegate(alice, val1, seiInUsei), stakingtypes.ErrNoDelegation)
			require.ErrorIs(t, pr.undelegate(alice, unknownValidator, seiInUsei), stakingtypes.ErrNoValidatorFound)
			require.NoError(t, pr.delegate(alice, val1, seiInUsei))
			require.ErrorIs(t, pr.undelegate(alice, val1, seiInUsei+1), sdkerrors.ErrInvalidRequest)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.run(t, newParityPair(t))
		})
	}
}

// TestValidatorLifecycleMatchesV2 takes validators in and out of the bonded
// set, through unbonding and removal, on the precompile and on x/staking,
// checking after every step.
func TestValidatorLifecycleMatchesV2(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(t *testing.T, pr *pair)
	}{
		{"an operator below its minimum self-delegation is outranked, unbonds and is removed", func(t *testing.T, pr *pair) {
			require.NoError(t, pr.delegate(alice, val3, seiInUsei))
			require.NoError(t, pr.undelegate(val3, val3, 19*seiInUsei+1))
			pr.endBlock()
			require.Equal(t, stakingtypes.Unbonding, pr.v2Status(val3))
			require.Equal(t, stakingtypes.Bonded, pr.v2Status(val4))
			require.NoError(t, pr.delegate(bob, val3, seiInUsei), "an unbonding validator takes delegations")
			pr.wait(parityParams.UnbondingTime)
			pr.endBlock()
			require.Equal(t, stakingtypes.Unbonded, pr.v2Status(val3))
			require.NoError(t, pr.undelegate(val3, val3, seiInUsei-1))
			require.NoError(t, pr.undelegate(alice, val3, seiInUsei))
			require.NoError(t, pr.undelegate(bob, val3, seiInUsei))
			_, ok := pr.v2.validator(val3)
			require.False(t, ok, "the last shares out remove an unbonded validator")
			require.ErrorIs(t, pr.delegate(alice, val3, seiInUsei), stakingtypes.ErrNoValidatorFound)
			pr.wait(parityParams.UnbondingTime)
			pr.endBlock()
		}},
		{"an operator with no self-delegation stays bonded on its delegators' stake", func(t *testing.T, pr *pair) {
			require.NoError(t, pr.delegate(alice, val2, 20*seiInUsei))
			require.NoError(t, pr.undelegate(val2, val2, 25*seiInUsei))
			pr.endBlock()
			require.Equal(t, stakingtypes.Bonded, pr.v2Status(val2))
		}},
		{"validators leaving the bonded set together unbond in address order", func(t *testing.T, pr *pair) {
			require.NoError(t, pr.undelegate(val3, val3, 20*seiInUsei))
			require.NoError(t, pr.undelegate(val2, val2, 25*seiInUsei))
			pr.endBlock()
			require.Equal(t, stakingtypes.Unbonding, pr.v2Status(val2))
			require.Equal(t, stakingtypes.Unbonding, pr.v2Status(val3))
			pr.wait(parityParams.UnbondingTime)
			pr.endBlock()
			_, ok := pr.v2.validator(val2)
			require.False(t, ok)
		}},
		{"an unbonding validator left with no shares is removed when its unbonding ends", func(t *testing.T, pr *pair) {
			require.NoError(t, pr.undelegate(val3, val3, 20*seiInUsei))
			pr.endBlock()
			require.Equal(t, stakingtypes.Unbonding, pr.v2Status(val3))
			pr.wait(parityParams.UnbondingTime)
			pr.endBlock()
			_, ok := pr.v2.validator(val3)
			require.False(t, ok)
		}},
		{"a validator bonded again while unbonding stays bonded when that unbonding would end", func(t *testing.T, pr *pair) {
			require.NoError(t, pr.delegate(alice, val4, 10*seiInUsei))
			pr.endBlock()
			require.Equal(t, stakingtypes.Unbonding, pr.v2Status(val3), "val4 outranks val3")
			require.NoError(t, pr.delegate(bob, val3, 10*seiInUsei))
			pr.endBlock()
			require.Equal(t, stakingtypes.Bonded, pr.v2Status(val3))
			require.Equal(t, stakingtypes.Unbonding, pr.v2Status(val4), "val2 ranks before val4 at the same power")
			pr.wait(parityParams.UnbondingTime)
			pr.endBlock()
			require.Equal(t, stakingtypes.Bonded, pr.v2Status(val3))
			require.Equal(t, stakingtypes.Unbonded, pr.v2Status(val4))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.run(t, newParityPair(t))
		})
	}
}

// TestRandomOperationsMatchV2 runs random delegations, undelegations and
// EndBlocks on the precompile and on x/staking, checking after every step, and
// requires the runs between them to reach each rule. x/staking's jailing is
// undone, since the precompile never jails.
func TestRandomOperationsMatchV2(t *testing.T) {
	const seeds = 6
	outcomes, ran := map[string]int{}, 0
	for seed := range int64(seeds) {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			ran++
			pr := newParityPair(t)
			randomOperations(pr, rand.New(rand.NewSource(seed)), 300) //nolint:gosec // deterministic test data
			for k, n := range pr.outcomes {
				outcomes[k] += n
			}
		})
	}
	// Only all the runs together reach every rule, and a failed run's mismatch
	// is the failure to read, so a failed or partial run skips this check.
	if t.Failed() || ran < seeds {
		return
	}
	for _, want := range []string{
		"delegate: ok",
		"delegate: " + stakingtypes.ErrNoValidatorFound.Error(),
		"delegate: " + stakingtypes.ErrExceedMaxVotingPowerRatio.Error(),
		"undelegate: ok",
		"undelegate: " + stakingtypes.ErrNoDelegation.Error(),
		"undelegate: " + sdkerrors.ErrInvalidRequest.Error(),
		"undelegate: " + stakingtypes.ErrMaxUnbondingDelegationEntries.Error(),
		"hook: AfterValidatorBonded",
		"hook: AfterValidatorBeginUnbonding",
		"hook: BeforeDelegationRemoved",
	} {
		assert.Positive(t, outcomes[want], "the runs reach %q", want)
	}
}

// randomOperations runs steps random operations on pr. val1's operator never
// acts, so val1 keeps its power and the bonded set never has none.
func randomOperations(pr *pair, rng *rand.Rand, steps int) {
	actors := []common.Address{alice, bob, carol, val2, val3, val4}
	targets := append(slices.Clone(pr.validators), unknownValidator)
	for range steps {
		d := pick(rng, actors)
		switch n := rng.Intn(100); {
		case n < 40:
			_ = pr.delegate(d, pick(rng, targets), randomDelegation(rng))
		case n < 80:
			op := pr.randomSource(rng, d, targets)
			_ = pr.undelegate(d, op, randomUnbond(rng, pr.v2.delegatedTokens(d, op)))
		case n < 95:
			pr.endBlock()
		default:
			pr.wait(uint64(rng.Int63n(int64(parityParams.UnbondingTime)))) //nolint:gosec // a positive duration
			pr.endBlock()
		}
	}
}

// randomSource returns, mostly, a validator delegator delegates to.
func (pr *pair) randomSource(rng *rand.Rand, delegator common.Address, targets []common.Address) common.Address {
	delegations := pr.v2.keeper.GetDelegatorDelegations(pr.v2.ctx, sdk.AccAddress(delegator[:]), 100)
	if len(delegations) == 0 || rng.Intn(5) == 0 {
		return pick(rng, targets)
	}
	return common.BytesToAddress(pick(rng, delegations).GetValidatorAddr())
}

// randomDelegation returns usei to delegate: mostly a few SEI, sometimes less
// than one unit of power, and sometimes enough to meet the voting power cap.
func randomDelegation(rng *rand.Rand) int64 {
	switch rng.Intn(10) {
	case 0:
		return rng.Int63n(seiInUsei) + 1
	case 1:
		return 10*seiInUsei + rng.Int63n(20*seiInUsei)
	default:
		return rng.Int63n(5*seiInUsei) + 1
	}
}

// randomUnbond returns usei to unbond from a delegation worth tokens: mostly
// part of it, sometimes all of it, and sometimes more than it holds.
func randomUnbond(rng *rand.Rand, tokens sdk.Int) int64 {
	if !tokens.IsPositive() {
		return rng.Int63n(seiInUsei) + 1
	}
	all := tokens.Int64()
	switch rng.Intn(10) {
	case 0:
		return all
	case 1:
		return all + rng.Int63n(seiInUsei) + 1
	default:
		return rng.Int63n(all) + 1
	}
}

func pick[T any](rng *rand.Rand, xs []T) T {
	return xs[rng.Intn(len(xs))]
}

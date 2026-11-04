package staking

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

func TestFirstEndBlockBondsTheTopValidators(t *testing.T) {
	h := newHarness(t, testParams)
	require.Equal(t, []validatorPower{{val1, 30}, {val2, 20}}, h.lastValidators())
	require.Equal(t, big.NewInt(50), h.store().lastTotalPower())
	require.Equal(t, statusBonded, h.validator(val1).status)
	require.Equal(t, statusBonded, h.validator(val2).status)
	require.Equal(t, statusUnbonded, h.validator(val3).status)
	require.Equal(t, sei(50), h.state.GetBalance(BondedEscrow))
	require.Equal(t, sei(10), h.state.GetBalance(NotBondedEscrow))
	h.requireInvariants()
}

func TestAValidatorOutrankedLeavesTheSetAndUnbonds(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val3, sei(15)))
	h.endBlock()
	require.Empty(t, h.failures)
	require.Equal(t, []validatorPower{{val1, 30}, {val3, 25}}, h.lastValidators())
	require.Equal(t, big.NewInt(55), h.store().lastTotalPower())
	require.Equal(t, statusBonded, h.validator(val3).status)
	v2 := h.validator(val2)
	require.Equal(t, statusUnbonding, v2.status)
	require.Equal(t, h.time-second+testParams.UnbondingTime, v2.unbondingTime)
	require.Equal(t, h.number-1, v2.unbondingHeight)
	h.requireInvariants(alice)

	h.advance(testParams.UnbondingTime - 2*second)
	require.Equal(t, statusUnbonding, h.validator(val2).status, "not before its unbonding time")
	h.endBlock()
	require.Empty(t, h.failures)
	require.Equal(t, statusUnbonded, h.validator(val2).status)
	q := h.store().validatorQueue()
	require.Equal(t, q.head(), q.tail())
	h.requireInvariants(alice)
}

func TestAValidatorThatBondsAgainSkipsItsOldQueueEntry(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val3, sei(15)))
	h.endBlock()
	require.Equal(t, statusUnbonding, h.validator(val2).status)
	require.NoError(t, h.delegate(bob, val2, sei(20)))
	h.endBlock()
	require.Equal(t, statusBonded, h.validator(val2).status)

	h.advance(testParams.UnbondingTime)
	require.Empty(t, h.failures)
	require.Equal(t, statusBonded, h.validator(val2).status, "the stale entry leaves it bonded")
	require.Equal(t, statusUnbonded, h.validator(val3).status, "val3's own entry completes")
	q := h.store().validatorQueue()
	require.Equal(t, q.head(), q.tail())
	h.requireInvariants(alice, bob)
}

func TestAnUnbondedValidatorWithNoSharesIsRemoved(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.undelegate(val3, val3, sei(10)))
	_, ok := h.store().validator(val3)
	require.False(t, ok)
	require.False(t, h.store().validatorSet().Contains(common.BytesToHash(val3[:])))
	require.Equal(t, common.Address{}, h.store().validatorByPubkey(common.BytesToHash(val3[:])))
	require.Equal(t, []common.Address{val1, val2}, h.powerIndex())
	for i := uint64(0); i < validatorSlots; i++ {
		require.NotContains(t, h.state.storage[Address], solstorage.Offset(validatorBase(val3), i), "slot %d of the record is cleared", i)
	}
	h.requireInvariants()

	h.advance(testParams.UnbondingTime)
	require.Empty(t, h.failures)
	require.Zero(t, h.state.GetBalance(NotBondedEscrow).Sign())
	require.Equal(t, sei(10), h.state.GetBalance(val3), "the unbonding entry outlives its validator")
}

func TestAValidatorFinishingUnbondingWithNoSharesIsRemoved(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.undelegate(val2, val2, sei(20)))
	h.endBlock()
	require.Equal(t, statusUnbonding, h.validator(val2).status)
	h.advance(testParams.UnbondingTime)
	require.Empty(t, h.failures)
	_, ok := h.store().validator(val2)
	require.False(t, ok)
	require.Equal(t, sei(20), h.state.GetBalance(val2))
	h.requireInvariants()
}

func TestUnbondingPaysOutOnceItsTimeComes(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.undelegate(alice, val1, sei(2)))
	h.advance(testParams.UnbondingTime - second)
	require.Zero(t, h.state.GetBalance(alice).Sign(), "not before its completion time")
	require.Len(t, must(h.store().unbondingEntries(alice, val1)), 1)
	h.endBlock()
	require.Empty(t, h.failures)
	require.Equal(t, sei(2), h.state.GetBalance(alice))
	require.Empty(t, must(h.store().unbondingEntries(alice, val1)))
	q := h.store().unbondingQueue()
	require.Equal(t, q.head(), q.tail())
	h.requireInvariants(alice)
}

func TestOneQueueEntryPaysEveryMaturedEntryOfItsDelegation(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.undelegate(alice, val1, sei(1)))
	require.NoError(t, h.undelegate(alice, val1, sei(2)))
	h.endBlock()
	require.NoError(t, h.undelegate(alice, val1, sei(1)))
	h.advance(testParams.UnbondingTime - second)
	require.Equal(t, sei(3), h.state.GetBalance(alice), "both entries of the first block, from its first queue entry")
	require.Len(t, must(h.store().unbondingEntries(alice, val1)), 1)
	h.endBlock()
	require.Empty(t, h.failures)
	require.Equal(t, sei(4), h.state.GetBalance(alice))
	require.Empty(t, must(h.store().unbondingEntries(alice, val1)))
	h.requireInvariants(alice)
}

func TestRedelegationCompletesWithoutMovingTokens(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.redelegate(alice, val1, val2, sei(2)))
	bonded := h.state.GetBalance(BondedEscrow)
	h.advance(testParams.UnbondingTime)
	require.Empty(t, h.failures)
	require.Zero(t, h.store().redelegationEntryCount(alice, val1, val2).Sign())
	require.Zero(t, h.store().receivingRedelegations(val2, alice).Sign())
	q := h.store().redelegationQueue()
	require.Equal(t, q.head(), q.tail())
	require.Equal(t, bonded, h.state.GetBalance(BondedEscrow))
	h.requireInvariants(alice)
}

func TestARedelegationFromAnUnbondingValidatorEndsWithIt(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val2, sei(5)))
	require.NoError(t, h.delegate(bob, val3, sei(20)))
	h.endBlock()
	v2 := h.validator(val2)
	require.Equal(t, statusUnbonding, v2.status)
	h.advance(10 * second)

	require.NoError(t, h.redelegate(alice, val2, val1, sei(5)))
	e := h.store().redelegationQueue().entry(0)
	require.Equal(t, v2.unbondingTime, h.store().Uint64(at(e, queueCompletionTime), queueCompletionTime.offset))
	require.Equal(t, v2.unbondingHeight, h.store().Uint64(at(e, redelegationQueueCreationHeight), redelegationQueueCreationHeight.offset))
	h.requireInvariants(alice, bob)
}

func TestAShorterUnbondingTimeQueuesBehindTheLongerOnes(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.undelegate(alice, val1, sei(1)))
	first := h.time + testParams.UnbondingTime
	require.NoError(t, chainparams.Apply(h.state, []chainparams.Change{{Subspace: "staking", Key: "UnbondingTime", Value: common.BigToHash(new(big.Int).SetUint64(10 * second)).Bytes()}}))
	h.endBlock()
	require.NoError(t, h.undelegate(alice, val1, sei(1)))
	entries := must(h.store().unbondingEntries(alice, val1))
	require.Equal(t, first, entries[1].completionTime, "the new entry waits for the one ahead of it")
	h.requireInvariants(alice)
}

func TestAShortPayoutIsReportedAndItsEntryKept(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.undelegate(alice, val1, sei(2)))
	require.NoError(t, h.delegate(bob, val1, sei(1)))
	require.NoError(t, h.undelegate(bob, val1, sei(1)))
	// The escrow holds val3's 10 SEI and the two entries' 3. Leave it 1 SEI:
	// short of alice's entry, ahead in the queue, but enough for bob's.
	require.NoError(t, h.state.SubBalance(NotBondedEscrow, sei(12)))
	h.advance(testParams.UnbondingTime)
	require.Len(t, h.failures, 1)
	require.ErrorIs(t, h.failures[0], precompiles.ErrDegraded)
	require.ErrorIs(t, h.failures[0], errEscrowShort)
	require.Zero(t, h.state.GetBalance(alice).Sign())
	require.Len(t, must(h.store().unbondingEntries(alice, val1)), 1, "the unpaid entry stays on record")
	require.Equal(t, sei(1), h.state.GetBalance(bob), "the next entry is paid")
	q := h.store().unbondingQueue()
	require.Equal(t, q.head(), q.tail(), "the failed queue entry is dropped")
}

func TestEndBlockBudgetDefersTheRest(t *testing.T) {
	h := newHarness(t, testParams)
	// Each entry costs two units: one for itself, one for the entry it pays.
	n := endBlockBudget/2 + 1
	delegators := make([]common.Address, n)
	s := h.store()
	for i := range delegators {
		d := common.BigToAddress(big.NewInt(int64(0x10000 + i)))
		delegators[i] = d
		h.fund(d, sei(1))
		v := h.validator(val1)
		_, err := h.staking.delegate(h.block(), s, testParams, d, val1, &v, sei(1), statusUnbonded, d)
		require.NoError(t, err)
		_, _, err = h.staking.undelegate(h.block(), s, testParams, d, val1, h.shares(d, val1))
		require.NoError(t, err)
	}
	h.advance(testParams.UnbondingTime - second)
	h.endBlock()
	require.Empty(t, h.failures)
	q := h.store().unbondingQueue()
	require.Equal(t, uint64(1), q.tail()-q.head(), "one entry waits for the next block")
	require.Zero(t, h.state.GetBalance(delegators[n-1]).Sign())
	h.endBlock()
	require.Equal(t, sei(1), h.state.GetBalance(delegators[n-1]))
	require.Equal(t, q.head(), q.tail())
}

func TestABondedSetWithNoPowerIsKept(t *testing.T) {
	params := testParams
	params.MaxValidators = 1
	h := newHarness(t, params)
	require.NoError(t, h.undelegate(val1, val1, sei(30)))
	require.NoError(t, h.undelegate(val2, val2, sei(20)))
	require.NoError(t, h.undelegate(val3, val3, sei(10)))
	h.endBlock()
	require.Len(t, h.failures, 1)
	require.ErrorIs(t, h.failures[0], precompiles.ErrDegraded)
	require.ErrorIs(t, h.failures[0], errNoBondedPower)
	require.Equal(t, []validatorPower{{val1, 30}}, h.lastValidators(), "the last set stands")
	require.Equal(t, statusBonded, h.validator(val1).status)
}

func TestTake(t *testing.T) {
	budget := uint64(endBlockBudget)
	require.True(t, take(&budget, endBlockBudget+1), "work larger than a whole budget runs on a fresh one")
	require.Zero(t, budget)
	budget = 10
	require.True(t, take(&budget, 10))
	require.False(t, take(&budget, 1))
	budget = 10
	require.False(t, take(&budget, 11))
	require.Equal(t, uint64(10), budget)
}

func TestACorruptQueueIsReported(t *testing.T) {
	h := newHarness(t, testParams)
	q := h.store().redelegationQueue()
	h.store().SetUint64(at(q.bounds, queueHead), queueHead.offset, 5)
	h.endBlock()
	require.Len(t, h.failures, 1)
	require.ErrorIs(t, h.failures[0], errQueueCorrupt)
	require.ErrorContains(t, h.failures[0], "redelegation queue")
}

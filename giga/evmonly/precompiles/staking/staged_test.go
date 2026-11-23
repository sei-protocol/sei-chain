package staking

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/dec"
)

// TestAStagedOperationTakesEffectAtTheEndOfTheNextEpoch stages delegations in
// epoch 0, one of them in the block that closes it, and one in epoch 1, and
// follows them and the epoch snapshots through the end of epochs 0, 1 and 2.
func TestAStagedOperationTakesEffectAtTheEndOfTheNextEpoch(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.stageDelegate(alice, val1, sei(5)))
	require.Zero(t, h.shares(alice, val1).Sign(), "a staged delegation waits")
	require.Equal(t, sei(30), h.validator(val1).tokens)
	require.Equal(t, sei(5), h.state.GetBalance(StagedEscrow))
	h.endBlock()
	require.NoError(t, h.stageDelegate(bob, val1, sei(1)), "staged in the block that closes epoch 0")
	h.endEpoch(0)
	require.Empty(t, h.failures)
	require.Zero(t, h.shares(alice, val1).Sign(), "the end of the epoch it was staged in is too soon")
	require.Equal(t, uint64(1), h.store().currentEpoch())
	require.Equal(t, []BondedValidator{
		{Operator: val1, ConsensusPubkey: common.BytesToHash(val1[:]), Power: 36},
		{Operator: val2, ConsensusPubkey: common.BytesToHash(val2[:]), Power: 20},
	}, must(EpochValidators(h.state)), "epoch 0's snapshot counts what will take effect at the end of epoch 1")
	require.NoError(t, h.stageDelegate(alice, val2, sei(2)))
	h.endBlock()
	require.Zero(t, h.shares(alice, val1).Sign())
	h.requireInvariants(alice, bob)

	h.endEpoch(1)
	require.Empty(t, h.failures)
	require.Equal(t, dec.FromInt(sei(5)), h.shares(alice, val1), "epoch 0's take effect at the end of epoch 1")
	require.Equal(t, dec.FromInt(sei(1)), h.shares(bob, val1))
	require.Zero(t, h.shares(alice, val2).Sign(), "epoch 1's wait for the end of epoch 2")
	require.Equal(t, []BondedValidator{
		{Operator: val1, ConsensusPubkey: common.BytesToHash(val1[:]), Power: 36},
		{Operator: val2, ConsensusPubkey: common.BytesToHash(val2[:]), Power: 22},
	}, must(EpochValidators(h.state)), "epoch 1's snapshot counts what will take effect at the end of epoch 2")
	h.requireInvariants(alice, bob)

	h.endEpoch(2)
	require.Empty(t, h.failures)
	require.Equal(t, dec.FromInt(sei(2)), h.shares(alice, val2))
	require.Zero(t, h.state.GetBalance(StagedEscrow).Sign())
	h.requireInvariants(alice, bob)
}

func TestStagingCountsWhatIsAlreadyStaged(t *testing.T) {
	params := testParams
	params.MaxEntries = 2
	h := newHarness(t, params)
	require.NoError(t, h.delegate(alice, val1, sei(10)))
	require.NoError(t, h.delegate(alice, val2, sei(2)))

	require.NoError(t, h.stageUndelegate(alice, val1, sei(6)))
	require.ErrorIs(t, h.stageUndelegate(alice, val1, sei(5)), errInvalidShares, "staged shares are spoken for")
	require.NoError(t, h.stageUndelegate(alice, val1, sei(1)))
	require.ErrorIs(t, h.stageUndelegate(alice, val1, sei(1)), errMaxUnbondingEntries, "staged undelegations count toward maxEntries")
	require.NoError(t, h.stageRedelegate(alice, val1, val2, sei(1)))
	require.ErrorIs(t, h.stageRedelegate(alice, val2, val3, sei(1)), errTransitiveRedelegation, "a staged redelegation into val2 blocks one out of it")
	require.NoError(t, h.stageRedelegate(alice, val1, val3, sei(1)))
	require.ErrorIs(t, h.stageRedelegate(alice, val1, val3, sei(1)), errMaxRedelegationEntries, "staged redelegations out of val1 count toward every destination's maxEntries")
	h.requireInvariants(alice)

	h.endEpoch(0)
	h.endEpoch(1)
	require.Empty(t, h.failures, "what staging allowed takes effect")
	require.Equal(t, dec.FromInt(sei(1)), h.shares(alice, val1))
	require.Equal(t, dec.FromInt(sei(3)), h.shares(alice, val2))
	require.Equal(t, dec.FromInt(sei(1)), h.shares(alice, val3))
	require.Len(t, must(h.store().unbondingEntries(alice, val1)), 2)
	h.requireInvariants(alice)
}

func TestAStagedDelegationThatCanNoLongerBeMadeIsRefunded(t *testing.T) {
	params := testParams
	params.MaxVotingPowerRatio = new(big.Int).Div(dec.One(), big.NewInt(2))
	params.MaxVotingPowerEnforcementThreshold = big.NewInt(1)
	h := newHarness(t, params)
	// val2 holds 20 of 50 bonded power. 8 more is 28 of 58, under one half, but
	// once bob's 8 are in, alice's 8 make 36 of 66.
	require.NoError(t, h.stageDelegate(bob, val2, sei(8)))
	require.NoError(t, h.stageDelegate(alice, val2, sei(8)), "each fits on its own")
	h.endEpoch(0)
	require.Empty(t, h.failures, "a drop in the projection is not reported")
	require.Equal(t, []BondedValidator{
		{Operator: val1, ConsensusPubkey: common.BytesToHash(val1[:]), Power: 30},
		{Operator: val2, ConsensusPubkey: common.BytesToHash(val2[:]), Power: 28},
	}, must(EpochValidators(h.state)), "epoch 0's snapshot leaves out the delegation that will be dropped")
	h.endEpoch(1)
	require.Len(t, h.failures, 1)
	require.ErrorIs(t, h.failures[0], precompiles.ErrDegraded)
	require.ErrorIs(t, h.failures[0], errExceedMaxVotingPower)
	require.Equal(t, dec.FromInt(sei(8)), h.shares(bob, val2))
	_, found := h.store().delegation(alice, val2)
	require.False(t, found)
	require.Equal(t, sei(8), h.state.GetBalance(alice), "alice's tokens come back")
	h.requireInvariants(alice, bob)
}

func TestProjectingAnEpochSnapshotLeavesTheStateAndHooksAsTheyWere(t *testing.T) {
	h := newHarness(t, testParams)
	var log hookLog
	h.staking.Hooks = loggingHooks{log: &log}
	// val3 has 10 and is unbonded; 15 more puts it ahead of val2's 20.
	require.NoError(t, h.stageDelegate(bob, val3, sei(15)))
	bonded, notBonded := h.state.GetBalance(BondedEscrow), h.state.GetBalance(NotBondedEscrow)

	h.endEpoch(0)
	require.Equal(t, []BondedValidator{
		{Operator: val1, ConsensusPubkey: common.BytesToHash(val1[:]), Power: 30},
		{Operator: val3, ConsensusPubkey: common.BytesToHash(val3[:]), Power: 25},
	}, must(EpochValidators(h.state)), "val3 is in the set the staged delegation will bond")
	require.Equal(t, statusUnbonded, h.validator(val3).status, "but is not bonded yet")
	require.Equal(t, sei(10), h.validator(val3).tokens)
	require.Zero(t, h.shares(bob, val3).Sign())
	require.Equal(t, bonded, h.state.GetBalance(BondedEscrow))
	require.Equal(t, notBonded, h.state.GetBalance(NotBondedEscrow))
	require.Equal(t, sei(15), h.state.GetBalance(StagedEscrow))
	q := h.store().stagedQueue()
	require.Equal(t, uint64(1), q.tail()-q.head(), "the operation is still staged")
	require.Empty(t, log, "the projection calls no hooks")
	h.requireInvariants(bob)

	h.endEpoch(1)
	require.Empty(t, h.failures)
	require.Equal(t, statusBonded, h.validator(val3).status)
	require.NotEmpty(t, log, "applying it calls them")
	h.requireInvariants(bob)
}

func TestStagedOperationsOnARemovedValidatorAreDropped(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.stageUndelegate(val3, val3, sei(10)))
	require.NoError(t, h.stageRedelegate(alice, val1, val3, sei(2)))
	require.NoError(t, h.stageDelegate(bob, val3, sei(1)))
	h.requireInvariants(alice, bob)

	h.endEpoch(0)
	h.endEpoch(1)
	_, ok := h.store().validator(val3)
	require.False(t, ok, "val3 undelegated its last shares while unbonded, which removed it")
	require.Len(t, h.failures, 2)
	require.ErrorIs(t, h.failures[0], errBadRedelegationValidator)
	require.ErrorIs(t, h.failures[1], errNoValidatorFound)
	require.Equal(t, dec.FromInt(sei(5)), h.shares(alice, val1), "the stake alice redelegated stays where it was")
	require.Equal(t, sei(1), h.state.GetBalance(bob), "bob's tokens come back")
	h.requireInvariants(alice, bob)
}

func TestStagedOperationsPastTheBudgetWaitForTheNextBlock(t *testing.T) {
	h := newHarness(t, testParams)
	s := h.store()
	n := endBlockBudget + 1
	for i := range n {
		h.fund(StagedEscrow, weiPerUsei)
		_, err := stage(s, stagedOperation{kind: stagedDelegate, delegator: common.BigToAddress(big.NewInt(int64(0x10000 + i))), validator: val3, amount: weiPerUsei})
		require.NoError(t, err)
	}
	h.endEpoch(0)
	h.endEpoch(1)
	require.Empty(t, h.failures)
	q := s.stagedQueue()
	require.Equal(t, uint64(1), q.tail()-q.head(), "one waits for the next block")
	h.endBlock()
	require.Empty(t, h.failures)
	require.Equal(t, q.head(), q.tail())
	require.Equal(t, new(big.Int).Add(sei(10), new(big.Int).Mul(weiPerUsei, big.NewInt(int64(n)))), h.validator(val3).tokens)
}

func TestTheEpochAdvancesWhenItsSnapshotFails(t *testing.T) {
	h := newHarness(t, testParams)
	h.store().setLastValidators([]validatorPower{{val1, 30}, {alice, 20}})
	h.endEpoch(0)
	require.ErrorIs(t, errors.Join(h.failures...), errUnknownValidator, "epoch 0 is not snapshotted")
	require.NoError(t, h.stageDelegate(bob, val1, sei(1)))
	q := h.store().stagedQueue()
	require.Equal(t, uint64(2), h.store().stagedOperationAt(q.entry(q.head())).epoch, "staged in epoch 1, it waits for the end of epoch 2")
}

func TestStagedTotalsThatDoNotAddUpAreReported(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.stageUndelegate(alice, val1, sei(2)))
	require.NoError(t, h.stageDelegate(bob, val1, sei(1)))
	h.store().setStagedDelegation(alice, val1, stagedDelegation{sharesOut: new(big.Int)})
	h.endEpoch(0)
	h.endEpoch(1)
	require.Len(t, h.failures, 2)
	require.ErrorIs(t, h.failures[0], errStagedNotCounted)
	require.ErrorIs(t, h.failures[1], errStagedNotCounted)
	require.ErrorContains(t, h.failures[1], "left unsettled")
	require.Equal(t, dec.FromInt(sei(5)), h.shares(alice, val1), "the undelegation is dropped")
	require.Equal(t, dec.FromInt(sei(1)), h.shares(bob, val1), "and holds up nothing behind it")
}

// TestAStagedDelegationCountsThePowerTheDelegationsBeforeItAdd applies two
// delegations to val2 whose sum stays under the voting power ratio once the
// first is counted in the bonded set's power as well as in val2's.
func TestAStagedDelegationCountsThePowerTheDelegationsBeforeItAdd(t *testing.T) {
	params := testParams
	params.MaxVotingPowerRatio = dec.Quo(dec.FromInt(big.NewInt(1)), dec.FromInt(big.NewInt(2)))
	params.MaxVotingPowerEnforcementThreshold = big.NewInt(1)
	h := newHarness(t, params)
	require.NoError(t, h.stageDelegate(bob, val2, sei(4)))
	require.NoError(t, h.stageDelegate(alice, val2, sei(4)))

	require.NoError(t, h.inEffect(nil), "28 of 58")
	require.Equal(t, sei(28), h.validator(val2).tokens)
	h.requireInvariants(alice, bob)
}

// TestAStagedDelegationIsCheckedAgainstThePowerTheOperationsBeforeItLeave
// applies an undelegation that takes 20 of the bonded set's 50, then a
// delegation the voting power ratio allows only against the 50.
func TestAStagedDelegationIsCheckedAgainstThePowerTheOperationsBeforeItLeave(t *testing.T) {
	params := testParams
	params.MaxVotingPowerRatio = dec.Quo(dec.FromInt(big.NewInt(1)), dec.FromInt(big.NewInt(2)))
	params.MaxVotingPowerEnforcementThreshold = big.NewInt(1)
	h := newHarness(t, params)
	require.NoError(t, h.stageUndelegate(val1, val1, sei(20)))
	require.NoError(t, h.stageDelegate(alice, val2, sei(10)), "30 of 60 is allowed when staged")

	require.ErrorIs(t, h.inEffect(nil), errExceedMaxVotingPower, "30 of 40 once val1's 20 has left")
	require.Equal(t, sei(10), h.validator(val1).tokens)
	require.Equal(t, sei(20), h.validator(val2).tokens)
	require.Equal(t, sei(10), h.state.GetBalance(alice), "the dropped delegation's tokens are returned")
	h.requireInvariants(alice)
}

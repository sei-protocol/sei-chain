package staking

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

func TestBondedValidatorsAreTheSetTheLastEndBlockStored(t *testing.T) {
	h := newHarness(t, testParams)
	want := []BondedValidator{
		{Operator: val1, ConsensusPubkey: common.BytesToHash(val1[:]), Power: 30},
		{Operator: val2, ConsensusPubkey: common.BytesToHash(val2[:]), Power: 20},
	}
	require.Equal(t, want, must(bondedValidators(h.store())))

	require.NoError(t, h.delegate(alice, val3, sei(15)))
	require.Equal(t, want, must(bondedValidators(h.store())), "a delegation moves the set only at EndBlock")
	h.endBlock()
	require.Equal(t, []BondedValidator{
		{Operator: val1, ConsensusPubkey: common.BytesToHash(val1[:]), Power: 30},
		{Operator: val3, ConsensusPubkey: common.BytesToHash(val3[:]), Power: 25},
	}, must(bondedValidators(h.store())), "the set moves at any EndBlock, not only an epoch's last")
}

func TestNoValidatorsAreBondedBeforeStakingBondsASet(t *testing.T) {
	state := newMemState()
	require.Empty(t, must(bondedValidators(newStore(state))))
	require.NoError(t, SetParams(state, testParams))
	require.Empty(t, must(bondedValidators(newStore(state))))
}

func TestBondedValidatorsFailOnAStoredSetThatDoesNotAddUp(t *testing.T) {
	h := newHarness(t, testParams)
	h.store().setLastValidators([]validatorPower{{val1, 30}, {alice, 20}})
	_, err := bondedValidators(h.store())
	require.ErrorIs(t, err, errUnknownValidator, "a member with no validator")

	h.store().SetUint64(solstorage.Slot(slotLastValidators), 0, 101)
	_, err = bondedValidators(h.store())
	require.ErrorIs(t, err, errInvalidStoredSet, "a set longer than any MaxValidators allows")
}

func TestEpochValidatorsAreTheSetAtTheLatestEpochEnd(t *testing.T) {
	h := newHarness(t, testParams)
	require.Empty(t, must(EpochValidators(h.state)), "no epoch has ended")

	h.endEpoch(0)
	want := []BondedValidator{
		{Operator: val1, ConsensusPubkey: common.BytesToHash(val1[:]), Power: 30},
		{Operator: val2, ConsensusPubkey: common.BytesToHash(val2[:]), Power: 20},
	}
	require.Equal(t, want, must(EpochValidators(h.state)))

	require.NoError(t, h.delegate(alice, val3, sei(15)))
	h.endBlock()
	require.Equal(t, want, must(EpochValidators(h.state)), "the bonded set moves within an epoch, its snapshot only at the epoch's end")
	h.endEpoch(1)
	require.Equal(t, []BondedValidator{
		{Operator: val1, ConsensusPubkey: common.BytesToHash(val1[:]), Power: 30},
		{Operator: val3, ConsensusPubkey: common.BytesToHash(val3[:]), Power: 25},
	}, must(EpochValidators(h.state)))
}

func TestEpochSnapshotsKeepTheLatestThree(t *testing.T) {
	h := newHarness(t, testParams)
	for epoch := range uint64(5) {
		h.endEpoch(epoch)
	}
	s := h.store()
	for epoch, kept := range []bool{false, false, true, true, true} {
		require.Equal(t, kept, len(must(s.epochValidators(uint64(epoch)))) > 0, "epoch %d", epoch)
	}
	latest, ok := s.latestSnapshotEpoch()
	require.True(t, ok)
	require.Equal(t, uint64(4), latest)
}

func TestNoEpochValidatorsBeforeStakingStarts(t *testing.T) {
	state := newMemState()
	require.Empty(t, must(EpochValidators(state)))
	require.NoError(t, SetParams(state, testParams))
	require.Empty(t, must(EpochValidators(state)))
}

func TestEpochValidatorsOutliveTheirValidators(t *testing.T) {
	h := newHarness(t, testParams)
	h.endEpoch(0)
	want := must(EpochValidators(h.state))

	require.NoError(t, h.delegate(alice, val3, sei(15)))
	h.endBlock()
	require.NoError(t, h.undelegate(val2, val2, sei(20)))
	h.advance(testParams.UnbondingTime)
	require.Empty(t, h.failures)
	_, ok := h.store().validator(val2)
	require.False(t, ok, "val2 left the bonded set and was removed once it unbonded with no shares")
	require.Equal(t, want, must(EpochValidators(h.state)))
}

func TestAnEpochWhoseSetDoesNotAddUpIsNotSnapshotted(t *testing.T) {
	h := newHarness(t, testParams)
	h.endEpoch(0)
	want := must(EpochValidators(h.state))
	h.store().setLastValidators([]validatorPower{{val1, 30}, {alice, 20}})
	h.endEpoch(1)
	require.ErrorIs(t, errors.Join(h.failures...), errUnknownValidator, "a member with no validator")
	require.Equal(t, want, must(EpochValidators(h.state)), "epoch 0's snapshot stays the latest")
}

func TestEpochValidatorsFailOnAStoredSetThatDoesNotAddUp(t *testing.T) {
	h := newHarness(t, testParams)
	h.endEpoch(0)
	h.store().SetUint64(solstorage.MapUint64(solstorage.Slot(slotEpochValidators), 0), 0, 101)
	_, err := EpochValidators(h.state)
	require.ErrorIs(t, err, errInvalidStoredSet, "a set longer than any MaxValidators allows")
}

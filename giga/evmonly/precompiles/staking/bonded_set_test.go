package staking

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

func TestBondedValidatorsAreTheSetTheLastEpochEndStored(t *testing.T) {
	h := newHarness(t, testParams)
	want := []BondedValidator{
		{Operator: val1, ConsensusPubkey: common.BytesToHash(val1[:]), Power: 30},
		{Operator: val2, ConsensusPubkey: common.BytesToHash(val2[:]), Power: 20},
	}
	require.Equal(t, want, must(bondedValidators(h.store())))

	require.NoError(t, h.delegate(alice, val3, sei(15)))
	require.Equal(t, want, must(bondedValidators(h.store())), "applying a delegation does not move the set")
	h.endBlock()
	require.Equal(t, want, must(bondedValidators(h.store())), "a block that closes no epoch leaves the set as it is")
	h.endCurrentEpoch()
	require.Equal(t, []BondedValidator{
		{Operator: val1, ConsensusPubkey: common.BytesToHash(val1[:]), Power: 30},
		{Operator: val3, ConsensusPubkey: common.BytesToHash(val3[:]), Power: 25},
	}, must(bondedValidators(h.store())), "the set moves at an epoch's end")
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

	require.NoError(t, h.stageDelegate(alice, val3, sei(15)))
	h.endBlock()
	require.Equal(t, want, must(EpochValidators(h.state)), "the snapshot moves only at an epoch's end")
	h.endEpoch(1)
	require.Equal(t, []BondedValidator{
		{Operator: val1, ConsensusPubkey: common.BytesToHash(val1[:]), Power: 30},
		{Operator: val3, ConsensusPubkey: common.BytesToHash(val3[:]), Power: 25},
	}, must(EpochValidators(h.state)))
}

func TestASmallerSnapshotClearsWhatItNoLongerCovers(t *testing.T) {
	h := newHarness(t, testParams)
	h.endEpoch(0)
	require.Len(t, must(EpochValidators(h.state)), 2)
	require.NoError(t, h.stageUndelegate(val2, val2, sei(20)))
	h.endEpoch(1)
	require.Equal(t, []BondedValidator{
		{Operator: val1, ConsensusPubkey: common.BytesToHash(val1[:]), Power: 30},
		{Operator: val3, ConsensusPubkey: common.BytesToHash(val3[:]), Power: 10},
	}, must(EpochValidators(h.state)), "val3 takes val2's place")
	require.NoError(t, h.stageUndelegate(val3, val3, sei(10)))
	h.endEpoch(2)
	require.Equal(t, []BondedValidator{
		{Operator: val1, ConsensusPubkey: common.BytesToHash(val1[:]), Power: 30},
	}, must(EpochValidators(h.state)))
	data := solstorage.ArrayData(solstorage.Slot(slotEpochValidators))
	for i := uint64(epochValidatorSlots); i < 2*epochValidatorSlots; i++ {
		require.Equal(t, common.Hash{}, h.store().Word(solstorage.Offset(data, i)), "slot %d of the second member", i)
	}
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
	require.NoError(t, h.store().deleteValidator(val2))
	require.Equal(t, want, must(EpochValidators(h.state)), "the snapshot holds val2's key itself")
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
	h.store().SetUint64(solstorage.Slot(slotEpochValidators), 0, 101)
	_, err := EpochValidators(h.state)
	require.ErrorIs(t, err, errInvalidStoredSet, "a set longer than any MaxValidators allows")
}

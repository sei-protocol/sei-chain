package staking

import (
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
	require.Equal(t, want, must(BondedValidators(h.state)))

	require.NoError(t, h.delegate(alice, val3, sei(15)))
	require.Equal(t, want, must(BondedValidators(h.state)), "a delegation moves the set only at EndBlock")
	h.endBlock()
	require.Equal(t, []BondedValidator{
		{Operator: val1, ConsensusPubkey: common.BytesToHash(val1[:]), Power: 30},
		{Operator: val3, ConsensusPubkey: common.BytesToHash(val3[:]), Power: 25},
	}, must(BondedValidators(h.state)))
}

func TestNoValidatorsAreBondedBeforeStakingBondsASet(t *testing.T) {
	state := newMemState()
	require.Empty(t, must(BondedValidators(state)))
	require.NoError(t, SetParams(state, testParams))
	require.Empty(t, must(BondedValidators(state)))
}

func TestBondedValidatorsFailOnAStoredSetThatDoesNotAddUp(t *testing.T) {
	h := newHarness(t, testParams)
	h.store().setLastValidators([]validatorPower{{val1, 30}, {alice, 20}})
	_, err := BondedValidators(h.state)
	require.ErrorIs(t, err, errUnknownValidator, "a member with no validator")

	h.store().SetUint64(solstorage.Slot(slotLastValidators), 0, 101)
	_, err = BondedValidators(h.state)
	require.ErrorIs(t, err, errInvalidStoredSet, "a set longer than any MaxValidators allows")
}

package params

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

// testEVM is a set of evm params whose two adjustments differ, so a swap between them shows.
var testEVM = EVM{
	MaxDynamicBaseFeeUpwardAdjustment:   big.NewInt(18_900_000_000_000_000),
	MaxDynamicBaseFeeDownwardAdjustment: big.NewInt(3_900_000_000_000_000),
}

func bigWord(v *big.Int) []byte {
	return v.FillBytes(make([]byte, 32))
}

func TestWriteEVMThenRead(t *testing.T) {
	state := newMemState()
	require.False(t, HasEVM(state))
	unwritten := ReadEVM(state)
	require.Zero(t, unwritten.MaxDynamicBaseFeeUpwardAdjustment.Sign(), "unwritten params read as zero")
	require.Zero(t, unwritten.MaxDynamicBaseFeeDownwardAdjustment.Sign(), "unwritten params read as zero")

	require.NoError(t, WriteEVM(state, testEVM))

	require.True(t, HasEVM(state))
	require.Equal(t, testEVM, ReadEVM(state))
	require.ErrorIs(t, WriteEVM(state, testEVM), errEVMWritten)
}

func TestParamReturnsTheAdjustmentsAsDecs(t *testing.T) {
	state := newGovState(t)
	require.NoError(t, WriteEVM(state, testEVM))

	up, err := call(t, state, ParamMethod, "evm", "KeyMaxDynamicBaseFeeUpwardAdjustment")
	require.NoError(t, err)
	down, err := call(t, state, ParamMethod, "evm", "KeyMaxDynamicBaseFeeDownwardAdjustment")
	require.NoError(t, err)

	require.Equal(t, bigWord(testEVM.MaxDynamicBaseFeeUpwardAdjustment), up[0])
	require.Equal(t, bigWord(testEVM.MaxDynamicBaseFeeDownwardAdjustment), down[0])
}

func TestEVMBounds(t *testing.T) {
	aboveOne := new(big.Int).Add(decOne, big.NewInt(1))
	for name, edit := range map[string]func(*EVM){
		"no upward adjustment":        func(e *EVM) { e.MaxDynamicBaseFeeUpwardAdjustment = nil },
		"zero upward adjustment":      func(e *EVM) { e.MaxDynamicBaseFeeUpwardAdjustment = new(big.Int) },
		"upward adjustment above 1":   func(e *EVM) { e.MaxDynamicBaseFeeUpwardAdjustment = aboveOne },
		"no downward adjustment":      func(e *EVM) { e.MaxDynamicBaseFeeDownwardAdjustment = nil },
		"downward adjustment above 1": func(e *EVM) { e.MaxDynamicBaseFeeDownwardAdjustment = aboveOne },
	} {
		e := testEVM
		edit(&e)
		require.Error(t, e.Validate(), name)
		state := newMemState()
		require.Error(t, WriteEVM(state, e), name)
		require.False(t, HasEVM(state), "%s: nothing is written", name)
	}
	require.Error(t, EVM{
		MaxDynamicBaseFeeUpwardAdjustment:   testEVM.MaxDynamicBaseFeeUpwardAdjustment,
		MaxDynamicBaseFeeDownwardAdjustment: big.NewInt(-1),
	}.Validate(), "negative downward adjustment")

	// Each adjustment may be 1, and the downward one may be 0.
	require.NoError(t, EVM{
		MaxDynamicBaseFeeUpwardAdjustment:   decOne,
		MaxDynamicBaseFeeDownwardAdjustment: new(big.Int),
	}.Validate())
	require.NoError(t, EVM{
		MaxDynamicBaseFeeUpwardAdjustment:   decOne,
		MaxDynamicBaseFeeDownwardAdjustment: decOne,
	}.Validate())
}

func TestApplyChangesTheEVMParams(t *testing.T) {
	state := newMemState()
	require.NoError(t, WriteEVM(state, testEVM))
	up := big.NewInt(6_000_000_000_000_000)
	down := big.NewInt(5_000_000_000_000_000)

	require.NoError(t, Apply(state, []Change{
		{Subspace: "evm", Key: "KeyMaxDynamicBaseFeeUpwardAdjustment", Value: bigWord(up)},
		{Subspace: "evm", Key: "KeyMaxDynamicBaseFeeDownwardAdjustment", Value: bigWord(down)},
	}))

	require.Equal(t, EVM{MaxDynamicBaseFeeUpwardAdjustment: up, MaxDynamicBaseFeeDownwardAdjustment: down}, ReadEVM(state))
}

func TestApplyRejectsAZeroUpwardAdjustment(t *testing.T) {
	state := newMemState()
	require.NoError(t, WriteEVM(state, testEVM))

	err := Apply(state, []Change{{Subspace: "evm", Key: "KeyMaxDynamicBaseFeeUpwardAdjustment", Value: bigWord(new(big.Int))}})

	require.ErrorIs(t, err, errInvalidValue)
	require.Equal(t, testEVM, ReadEVM(state))
}

func TestChangesKeepTheDownwardAdjustmentFloor(t *testing.T) {
	state := newMemState()
	require.NoError(t, WriteEVM(state, testEVM))
	floor := Change{"evm", "KeyMaxDynamicBaseFeeDownwardAdjustment", word(MinChangedBaseFeeDownwardAdjustment)}
	below := Change{"evm", "KeyMaxDynamicBaseFeeDownwardAdjustment", word(MinChangedBaseFeeDownwardAdjustment - 1)}
	require.ErrorIs(t, CheckChanges([]Change{below}), errInvalidValue)
	require.ErrorIs(t, Apply(state, []Change{below}), errInvalidValue)
	require.Equal(t, testEVM, ReadEVM(state))
	require.NoError(t, Apply(state, []Change{floor}))
	require.Zero(t, ReadEVM(state).MaxDynamicBaseFeeDownwardAdjustment.Cmp(new(big.Int).SetUint64(MinChangedBaseFeeDownwardAdjustment)))

	noDecay := testEVM
	noDecay.MaxDynamicBaseFeeDownwardAdjustment = new(big.Int)
	require.NoError(t, WriteEVM(newMemState(), noDecay), "genesis may set a downward adjustment below the floor")
}

func TestEVMValuesRoundTrip(t *testing.T) {
	require.Equal(t, testEVM, evmFrom(testEVM.values()))
}

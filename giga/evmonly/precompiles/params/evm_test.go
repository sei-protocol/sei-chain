package params

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

// testEVM is the evm subspace's launch params.
var testEVM = EVM{
	MinFeePerGas:             big.NewInt(100_000_000_000),
	TargetGasUsedPerSecond:   2_000_000_000,
	BaseFeeChangeDenominator: 144,
}

func dec(wei int64) []byte {
	return bigWord(new(big.Int).Mul(big.NewInt(wei), decOne))
}

func bigWord(v *big.Int) []byte {
	return v.FillBytes(make([]byte, 32))
}

func TestWriteEVMThenRead(t *testing.T) {
	state := newMemState()
	require.False(t, HasEVM(state))
	require.Equal(t, EVM{MinFeePerGas: new(big.Int)}, ReadEVM(state), "unwritten params read as zero")

	require.NoError(t, WriteEVM(state, testEVM))

	require.True(t, HasEVM(state))
	require.Equal(t, testEVM, ReadEVM(state))
	require.ErrorIs(t, WriteEVM(state, testEVM), errEVMWritten)
}

// The floor is stored as a Dec, so param returns it scaled by 1e18.
func TestParamReturnsTheMinFeePerGasAsADec(t *testing.T) {
	state := newGovState(t)
	require.NoError(t, WriteEVM(state, testEVM))

	out, err := call(t, state, ParamMethod, "evm", "KeyMinFeePerGas")

	require.NoError(t, err)
	require.Equal(t, dec(100_000_000_000), out[0])
}

func TestEVMValidate(t *testing.T) {
	require.NoError(t, testEVM.Validate())

	for name, mutate := range map[string]func(*EVM){
		"no floor":         func(e *EVM) { e.MinFeePerGas = nil },
		"negative floor":   func(e *EVM) { e.MinFeePerGas = big.NewInt(-1) },
		"zero target":      func(e *EVM) { e.TargetGasUsedPerSecond = 0 },
		"zero denominator": func(e *EVM) { e.BaseFeeChangeDenominator = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			e := testEVM
			mutate(&e)
			require.Error(t, e.Validate())
			require.Error(t, WriteEVM(newMemState(), e))
		})
	}
}

func TestWriteEVMTakesAZeroFloor(t *testing.T) {
	state := newMemState()
	zero := testEVM
	zero.MinFeePerGas = new(big.Int)

	require.NoError(t, WriteEVM(state, zero))

	require.True(t, HasEVM(state))
	require.Equal(t, zero, ReadEVM(state))
}

// The floor has no bound short of the largest whole number of wei a Dec holds.
func TestWriteEVMTakesTheHighestFloor(t *testing.T) {
	state := newMemState()
	highest := testEVM
	maxWord := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	highest.MinFeePerGas = new(big.Int).Quo(maxWord, decOne)

	require.NoError(t, WriteEVM(state, highest))

	require.Equal(t, highest, ReadEVM(state))
}

// One wei above the highest floor does not fit a Dec, and nothing is written.
func TestWriteEVMRejectsAFloorThatDoesNotFit(t *testing.T) {
	state := newMemState()
	tooHigh := testEVM
	maxWord := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	tooHigh.MinFeePerGas = new(big.Int).Add(new(big.Int).Quo(maxWord, decOne), big.NewInt(1))

	require.ErrorIs(t, WriteEVM(state, tooHigh), errInvalidValue)

	require.False(t, HasEVM(state))
}

func TestApplyChangesTheEVMParams(t *testing.T) {
	state := newMemState()
	require.NoError(t, WriteEVM(state, testEVM))

	require.NoError(t, Apply(state, []Change{
		{Subspace: "evm", Key: "KeyMinFeePerGas", Value: dec(200_000_000_000)},
		{Subspace: "evm", Key: "KeyTargetGasUsedPerSecond", Value: word(1_000_000_000)},
	}))

	require.Equal(t, EVM{
		MinFeePerGas:             big.NewInt(200_000_000_000),
		TargetGasUsedPerSecond:   1_000_000_000,
		BaseFeeChangeDenominator: 144,
	}, ReadEVM(state))
}

func TestApplyRejectsAFractionalMinFeePerGas(t *testing.T) {
	state := newMemState()
	require.NoError(t, WriteEVM(state, testEVM))
	fractional := new(big.Int).Add(new(big.Int).Mul(big.NewInt(100_000_000_000), decOne), big.NewInt(1))

	err := Apply(state, []Change{{Subspace: "evm", Key: "KeyMinFeePerGas", Value: bigWord(fractional)}})

	require.ErrorIs(t, err, errInvalidValue)
	require.Equal(t, testEVM, ReadEVM(state))
}

func TestEVMValuesRoundTrip(t *testing.T) {
	require.Equal(t, testEVM, evmFrom(testEVM.values()))
}

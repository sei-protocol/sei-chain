package params

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

// testEVM is a set of evm params whose Decs all differ, so a swap between them shows.
var testEVM = EVM{
	MaxDynamicBaseFeeUpwardAdjustment:   big.NewInt(18_900_000_000_000_000),
	MaxDynamicBaseFeeDownwardAdjustment: big.NewInt(3_900_000_000_000_000),
	MinFeePerGas:                        new(big.Int).Mul(big.NewInt(1_000_000_000), decOne),
	BaseFeeLag:                          3,
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
	require.Zero(t, unwritten.MinFeePerGas.Sign(), "unwritten params read as zero")
	require.Zero(t, unwritten.BaseFeeLag, "unwritten params read as zero")

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
	minFee, err := call(t, state, ParamMethod, "evm", "KeyMinFeePerGas")
	require.NoError(t, err)
	lag, err := call(t, state, ParamMethod, "evm", "KeyBaseFeeLag")
	require.NoError(t, err)

	require.Equal(t, bigWord(testEVM.MaxDynamicBaseFeeUpwardAdjustment), up[0])
	require.Equal(t, bigWord(testEVM.MaxDynamicBaseFeeDownwardAdjustment), down[0])
	require.Equal(t, bigWord(testEVM.MinFeePerGas), minFee[0])
	require.Equal(t, word(uint64(testEVM.BaseFeeLag)), lag[0])
}

func TestEVMBounds(t *testing.T) {
	aboveOne := new(big.Int).Add(decOne, big.NewInt(1))
	minDown := new(big.Int).SetUint64(MinBaseFeeDownwardAdjustment)
	for name, edit := range map[string]func(*EVM){
		"no upward adjustment":                func(e *EVM) { e.MaxDynamicBaseFeeUpwardAdjustment = nil },
		"zero upward adjustment":              func(e *EVM) { e.MaxDynamicBaseFeeUpwardAdjustment = new(big.Int) },
		"upward adjustment above 1":           func(e *EVM) { e.MaxDynamicBaseFeeUpwardAdjustment = aboveOne },
		"no downward adjustment":              func(e *EVM) { e.MaxDynamicBaseFeeDownwardAdjustment = nil },
		"zero downward adjustment":            func(e *EVM) { e.MaxDynamicBaseFeeDownwardAdjustment = new(big.Int) },
		"downward adjustment below its least": func(e *EVM) { e.MaxDynamicBaseFeeDownwardAdjustment = new(big.Int).Sub(minDown, big.NewInt(1)) },
		"negative downward adjustment":        func(e *EVM) { e.MaxDynamicBaseFeeDownwardAdjustment = big.NewInt(-1) },
		"downward adjustment above 1":         func(e *EVM) { e.MaxDynamicBaseFeeDownwardAdjustment = aboveOne },
		"no minimum fee":                      func(e *EVM) { e.MinFeePerGas = nil },
		"negative minimum fee":                func(e *EVM) { e.MinFeePerGas = big.NewInt(-1) },
		"minimum fee of part of a wei":        func(e *EVM) { e.MinFeePerGas = new(big.Int).Add(decOne, big.NewInt(1)) },
		"zero lag":                            func(e *EVM) { e.BaseFeeLag = 0 },
		"lag above its most":                  func(e *EVM) { e.BaseFeeLag = MaxBaseFeeLag + 1 },
	} {
		e := testEVM
		edit(&e)
		require.Error(t, e.Validate(), name)
		state := newMemState()
		require.Error(t, WriteEVM(state, e), name)
		require.False(t, HasEVM(state), "%s: nothing is written", name)
	}

	// Each adjustment may be 1, the downward one as little as its least, the minimum fee 0, and
	// the lag anywhere in its bounds.
	for name, edit := range map[string]func(*EVM){
		"adjustments of 1": func(e *EVM) {
			e.MaxDynamicBaseFeeUpwardAdjustment, e.MaxDynamicBaseFeeDownwardAdjustment = decOne, decOne
		},
		"least downward adjustment": func(e *EVM) { e.MaxDynamicBaseFeeDownwardAdjustment = minDown },
		"zero minimum fee":          func(e *EVM) { e.MinFeePerGas = new(big.Int) },
		"least lag":                 func(e *EVM) { e.BaseFeeLag = MinBaseFeeLag },
		"most lag":                  func(e *EVM) { e.BaseFeeLag = MaxBaseFeeLag },
	} {
		e := testEVM
		edit(&e)
		require.NoError(t, e.Validate(), name)
	}
}

func TestApplyChangesTheEVMParams(t *testing.T) {
	state := newMemState()
	require.NoError(t, WriteEVM(state, testEVM))
	up := big.NewInt(6_000_000_000_000_000)
	down := big.NewInt(5_000_000_000_000_000)
	minFee := new(big.Int).Mul(big.NewInt(2_000_000_000), decOne)

	require.NoError(t, Apply(state, []Change{
		{Subspace: "evm", Key: "KeyMaxDynamicBaseFeeUpwardAdjustment", Value: bigWord(up)},
		{Subspace: "evm", Key: "KeyMaxDynamicBaseFeeDownwardAdjustment", Value: bigWord(down)},
		{Subspace: "evm", Key: "KeyMinFeePerGas", Value: bigWord(minFee)},
		{Subspace: "evm", Key: "KeyBaseFeeLag", Value: word(5)},
	}))

	require.Equal(t, EVM{
		MaxDynamicBaseFeeUpwardAdjustment:   up,
		MaxDynamicBaseFeeDownwardAdjustment: down,
		MinFeePerGas:                        minFee,
		BaseFeeLag:                          5,
	}, ReadEVM(state))
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
	floor := Change{"evm", "KeyMaxDynamicBaseFeeDownwardAdjustment", word(MinBaseFeeDownwardAdjustment)}
	below := Change{"evm", "KeyMaxDynamicBaseFeeDownwardAdjustment", word(MinBaseFeeDownwardAdjustment - 1)}
	require.ErrorIs(t, CheckChanges([]Change{below}), errInvalidValue, "the range check refuses it without reading state")
	require.ErrorIs(t, Apply(state, []Change{below}), errInvalidValue)
	require.Equal(t, testEVM, ReadEVM(state))
	require.NoError(t, Apply(state, []Change{floor}))
	require.Zero(t, ReadEVM(state).MaxDynamicBaseFeeDownwardAdjustment.Cmp(new(big.Int).SetUint64(MinBaseFeeDownwardAdjustment)))
}

func TestApplyRejectsAPartWeiMinimumFee(t *testing.T) {
	state := newMemState()
	require.NoError(t, WriteEVM(state, testEVM))

	err := Apply(state, []Change{{Subspace: "evm", Key: "KeyMinFeePerGas", Value: bigWord(new(big.Int).Add(testEVM.MinFeePerGas, big.NewInt(1)))}})

	require.Error(t, err)
	require.Equal(t, testEVM, ReadEVM(state))
}

func TestEVMValuesRoundTrip(t *testing.T) {
	require.Equal(t, testEVM, evmFrom(testEVM.values()))
}

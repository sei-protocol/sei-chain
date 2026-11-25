package upgrade

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

func call(t *testing.T, state precompiles.State, method string, args ...interface{}) ([]interface{}, error) {
	t.Helper()
	input, err := ABI.Pack(method, args...)
	require.NoError(t, err)
	out, err := Precompile{}.Run(&precompiles.Context{Address: Address, ReadOnly: true, State: state}, input)
	if errors.Is(err, vm.ErrExecutionReverted) {
		reason, unpackErr := abi.UnpackRevert(out)
		require.NoError(t, unpackErr)
		return nil, fmt.Errorf("%w: %s", err, reason)
	}
	if err != nil {
		return nil, err
	}
	return ABI.Methods[method].Outputs.Unpack(out)
}

// abiPlanOut is the struct go-ethereum unpacks IUpgrade.Plan into.
type abiPlanOut = struct {
	Name   string `json:"name"`
	Height uint64 `json:"height"`
	Info   string `json:"info"`
}

func TestCurrentPlan(t *testing.T) {
	state := newMemState()
	out, err := call(t, state, CurrentPlanMethod)
	require.NoError(t, err)
	require.Equal(t, abiPlanOut{}, out[0], "no plan reads as a zero plan")

	require.NoError(t, Schedule(state, 1, Plan{Name: "v7.0", Height: 20, Info: `{"upgradeType":"minor"}`}))
	out, err = call(t, state, CurrentPlanMethod)
	require.NoError(t, err)
	require.Equal(t, abiPlanOut{Name: "v7.0", Height: 20, Info: `{"upgradeType":"minor"}`}, out[0])
}

func TestAppliedPlan(t *testing.T) {
	state := newMemState()
	require.NoError(t, WriteGenesis(state, []string{"v6.8"}, 3))
	out, err := call(t, state, AppliedPlanMethod, "v6.8")
	require.NoError(t, err)
	require.Equal(t, uint64(3), out[0])
	out, err = call(t, state, AppliedPlanMethod, "v7.0")
	require.NoError(t, err)
	require.Equal(t, uint64(0), out[0])

	_, err = call(t, state, AppliedPlanMethod, strings.Repeat("n", MaxNameLength+1))
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
}

func TestModuleVersionsReverts(t *testing.T) {
	_, err := call(t, newMemState(), "moduleVersions", "")
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.ErrorContains(t, err, retiredMethods["moduleVersions(string)"])
}

func TestRunRejectsValueAndDelegateCall(t *testing.T) {
	input, err := ABI.Pack(CurrentPlanMethod)
	require.NoError(t, err)
	_, err = Precompile{}.Run(&precompiles.Context{State: newMemState(), ApparentValue: big.NewInt(1)}, input)
	require.ErrorIs(t, err, errNonPayable)
	_, err = Precompile{}.Run(&precompiles.Context{State: newMemState(), DelegateCall: true}, input)
	require.ErrorIs(t, err, errDelegateCall)
	_, err = Precompile{}.Run(&precompiles.Context{State: newMemState()}, []byte{1, 2, 3})
	require.ErrorIs(t, err, errUnknownMethod)
}

func TestEveryMethodIsServedOrRetired(t *testing.T) {
	for name, method := range ABI.Methods {
		require.Equal(t, method.RawName, name, "no overload is renamed by go-ethereum")
		if _, retired := retiredMethods[method.Sig]; retired {
			continue
		}
		args := make([]interface{}, len(method.Inputs))
		for i, in := range method.Inputs {
			args[i] = reflect.New(in.Type.GetType()).Elem().Interface()
		}
		input, err := method.Inputs.Pack(args...)
		require.NoError(t, err, name)
		_, err = Precompile{}.Run(&precompiles.Context{Address: Address, State: newMemState()}, append(method.ID, input...))
		require.NoError(t, err, name)
	}
}

func TestABIKeepsV2Methods(t *testing.T) {
	bz, err := os.ReadFile("../../../../precompiles/upgrade/abi.json")
	require.NoError(t, err)
	v2, err := abi.JSON(bytes.NewReader(bz))
	require.NoError(t, err)
	require.NotEmpty(t, v2.Methods)
	for _, m := range v2.Methods {
		ours, ok := ABI.Methods[m.Name]
		require.True(t, ok, m.Sig)
		require.Equal(t, m.ID, ours.ID, m.Sig)
		require.Equal(t, abiShape(m.Outputs, true), abiShape(ours.Outputs, false), m.Sig)
	}
}

// abiShape returns the name and type of each of args and of every tuple field
// within them. With v2 set, an int64 named height reads as uint64.
func abiShape(args abi.Arguments, v2 bool) []string {
	var shape []string
	for _, arg := range args {
		shape = appendABIShape(shape, arg.Name, arg.Type, v2)
	}
	return shape
}

// appendABIShape appends the name and type of typ, then those of its tuple
// fields, to shape.
func appendABIShape(shape []string, name string, typ abi.Type, v2 bool) []string {
	switch {
	case typ.T == abi.TupleTy:
		shape = append(shape, name+" tuple "+typ.TupleRawName)
	case typ.T == abi.SliceTy && typ.Elem.T == abi.TupleTy:
		typ = *typ.Elem
		shape = append(shape, name+" tuple[] "+typ.TupleRawName)
	case v2 && name == "height" && typ.String() == "int64":
		return append(shape, name+" uint64")
	default:
		return append(shape, name+" "+typ.String())
	}
	for i, field := range typ.TupleElems {
		shape = appendABIShape(shape, typ.TupleRawNames[i], *field, v2)
	}
	return shape
}

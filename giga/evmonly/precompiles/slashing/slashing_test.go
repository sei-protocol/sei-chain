package slashing

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
)

func TestUnjailReturnsTheCallersValidatorToTheBondedSet(t *testing.T) {
	h := newHarness(t)
	h.stake(val1, nil, staking.UndelegateMethod, val1, sei(6))
	h.endBlock()
	require.Equal(t, []common.Address{val2}, h.bonded(), "val1 is jailed")

	_, err := h.unjail(val1, callOpts{})
	require.Error(t, err, "val1's self-delegation is below its minimum")
	h.stake(val1, sei(1), staking.DelegateMethod, val1)

	out, err := h.unjail(val1, callOpts{})
	require.NoError(t, err)
	values, err := ABI.Methods[UnjailMethod].Outputs.Unpack(out)
	require.NoError(t, err)
	require.Equal(t, []interface{}{true}, values)
	log := h.state.logs[len(h.state.logs)-1]
	require.Equal(t, Address, log.Address)
	require.Equal(t, []common.Hash{ABI.Events[UnjailEvent].ID, common.BytesToHash(val1[:])}, log.Topics)
	require.Empty(t, log.Data)

	h.endBlock()
	require.Equal(t, []common.Address{val2, val1}, h.bonded(), "val1 bonds again")
}

func TestUnjailFailsWithoutRevertDataAgainstTheState(t *testing.T) {
	h := newHarness(t)
	logs := len(h.state.logs)
	for _, caller := range []common.Address{val1, alice} {
		out, err := h.unjail(caller, callOpts{})
		require.Error(t, err, "%s has no jailed validator", caller)
		require.NotErrorIs(t, err, vm.ErrExecutionReverted)
		require.Nil(t, out)
	}
	require.Len(t, h.state.logs, logs)
}

func TestRunRefusesDelegateCallStaticCallAndValue(t *testing.T) {
	h := newHarness(t)
	_, err := h.unjail(val1, callOpts{delegateCall: true})
	require.ErrorIs(t, err, errDelegateCall)
	_, err = h.unjail(val1, callOpts{readOnly: true})
	require.ErrorIs(t, err, vm.ErrWriteProtection)
	h.state.AddBalance(val1, sei(1))
	_, err = h.unjail(val1, callOpts{value: sei(1)})
	require.ErrorIs(t, err, errNonPayable)
	_, err = h.run(Precompile{}, Address, val1, callOpts{}, []byte{1, 2, 3})
	require.ErrorIs(t, err, errUnknownMethod)
}

func TestRetiredMethodsRevertWithAReason(t *testing.T) {
	h := newHarness(t)
	for sig, reason := range retiredMethods {
		method := methodBySig(t, sig)
		args := make([]interface{}, len(method.Inputs))
		for i, in := range method.Inputs {
			args[i] = reflect.New(in.Type.GetType()).Elem().Interface()
		}
		input, err := method.Inputs.Pack(args...)
		require.NoError(t, err, sig)
		out, err := h.run(Precompile{}, Address, val1, callOpts{}, append(method.ID, input...))
		require.ErrorIs(t, err, vm.ErrExecutionReverted, sig)
		got, err := abi.UnpackRevert(out)
		require.NoError(t, err, sig)
		require.Equal(t, reason, got, sig)
	}
}

// TestABIKeepsV2Methods pins every slashing method to a signature of
// precompiles/slashing, so callers of V2's selectors reach it.
func TestABIKeepsV2Methods(t *testing.T) {
	bz, err := os.ReadFile("../../../../precompiles/slashing/abi.json")
	require.NoError(t, err)
	v2, err := abi.JSON(bytes.NewReader(bz))
	require.NoError(t, err)
	for _, m := range ABI.Methods {
		var found bool
		for _, v2m := range v2.Methods {
			found = found || v2m.Sig == m.Sig
		}
		require.True(t, found, "%s is a V2 method", m.Sig)
	}
	for sig := range retiredMethods {
		require.Equal(t, sig, methodBySig(t, sig).Sig)
	}
}

func TestRequiredGas(t *testing.T) {
	input, err := ABI.Pack(UnjailMethod)
	require.NoError(t, err)
	require.Equal(t, uint64(transactionGas), Precompile{}.RequiredGas(input))
	input, err = ABI.Pack("unjailWithAuthorization", val1)
	require.NoError(t, err)
	require.Equal(t, uint64(transactionGas+transactionByteGas*32), Precompile{}.RequiredGas(input))
	require.Equal(t, uint64(unknownMethodGas), Precompile{}.RequiredGas([]byte{1}))
}

func methodBySig(t *testing.T, sig string) abi.Method {
	for _, m := range ABI.Methods {
		if m.Sig == sig {
			return m
		}
	}
	t.Fatalf("no method %s", sig)
	return abi.Method{}
}

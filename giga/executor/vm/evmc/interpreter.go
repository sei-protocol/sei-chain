package evmc

import (
	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/core/vm"
)

var _ vm.IEVMInterpreter = (*EVMInterpreter)(nil)

// EVMInterpreter is a custom interpreter that delegates execution to evmone via EVMC.
type EVMInterpreter struct {
	hostContext *HostContext
	evm         *vm.EVM
	readOnly    bool
}

func NewEVMInterpreter(hostContext *HostContext, evm *vm.EVM) *EVMInterpreter {
	return &EVMInterpreter{hostContext: hostContext, evm: evm}
}

// Run executes the contract code via evmone.
func (e *EVMInterpreter) Run(callOpCode vm.OpCode, contract *vm.Contract, input []byte, readOnly bool) ([]byte, error) {
	// Increment the call depth which is restricted to 1024
	e.evm.Depth++
	defer func() { e.evm.Depth-- }()
	depth := e.evm.Depth

	// Make sure the readOnly is only set if we aren't in readOnly yet.
	// This also makes sure that the readOnly flag isn't removed for child calls.
	if readOnly && !e.readOnly {
		e.readOnly = true
		defer func() { e.readOnly = false }()
	}

	var static bool
	if callOpCode == vm.STATICCALL {
		static = true
	}

	var callKind evmc.CallKind
	switch callOpCode {
	case vm.STATICCALL:
		fallthrough
	case vm.CALL:
		callKind = evmc.Call
	case vm.DELEGATECALL:
		callKind = evmc.DelegateCall
	case vm.CREATE2:
		callKind = evmc.Create2
	case vm.CREATE:
		callKind = evmc.Create
	case vm.CALLCODE:
		callKind = evmc.CallCode
	default:
		panic("unsupported call type")
	}

	// todo(pdrobnjak): sender and recipient might not be correctly propagated in case of DELEGATECALL
	sender := evmc.Address(contract.Caller())
	recipient := evmc.Address(contract.Address())

	//nolint:dogsled,gosec // dogsled: Call returns 5 values, we only need output and err; gosec: safe gas conversion
	output, _, _, _, err := e.hostContext.Execute(callKind, recipient, sender, contract.Value().Bytes32(), input,
		int64(contract.Gas), depth, static)
	if err != nil {
		return nil, err
	}

	return output, nil
}

func (e *EVMInterpreter) ReadOnly() bool {
	return e.readOnly
}

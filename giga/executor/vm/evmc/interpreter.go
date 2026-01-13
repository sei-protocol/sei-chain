package evmc

import (
	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/core/vm"
)

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
func (e *EVMInterpreter) Run(contract *vm.Contract, input []byte, readOnly bool) ([]byte, error) {
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

	// todo(pdrobnjak): figure out how to access these values and how to validate if they are populated correctly
	callKind := evmc.Call
	recipient := evmc.Address{}
	sender := evmc.Address{}
	static := false
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

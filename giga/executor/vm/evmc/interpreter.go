package evmc

import (
	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/core/vm"
)

// EVMInterpreter is a custom interpreter that delegates execution to evmone via EVMC.
// Note: This cannot replace geth's interpreter directly since the interpreter field is unexported.
// Instead, this is used as a helper for the HostContext's Call method.
type EVMInterpreter struct {
	hostContext evmc.HostContext
	evm         *vm.EVM
	readOnly    bool
}

func NewEVMInterpreter(hostContext evmc.HostContext, evm *vm.EVM) *EVMInterpreter {
	return &EVMInterpreter{hostContext: hostContext, evm: evm}
}

// Run executes the contract code via evmone.
// Note: This is not currently used as the main execution path since we can't replace
// geth's interpreter. The execution flows through StateTransition -> geth interpreter.
// This is kept for future use when we implement direct evmone integration.
func (e *EVMInterpreter) Run(contract *vm.Contract, input []byte, readOnly bool) ([]byte, error) {
	// Increment the call depth which is restricted to 1024
	// Note: We use GetDepth() since depth field is unexported
	depth := e.evm.GetDepth()

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
	// irrelevant as it is only used for CREATE2 - geth is handling our CREATE2 logic
	salt := evmc.Hash{}
	codeAddress := evmc.Address{}
	output, _, _, _, err := e.hostContext.Call(callKind, recipient, sender, contract.Value().Bytes32(), input,
		int64(contract.Gas), depth, static, salt, codeAddress)
	if err != nil {
		return nil, err
	}

	return output, nil
}

func (e *EVMInterpreter) ReadOnly() bool {
	return e.readOnly
}

package evmc

import (
	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/core/vm"
)

var _ vm.IEVMInterpreter = (*EVMInterpreter)(nil)

type EVMInterpreter struct {
	hostContext evmc.HostContext
	evm         *vm.EVM
	readOnly    bool
}

func NewEVMInterpreter(hostContext evmc.HostContext, evm *vm.EVM) *EVMInterpreter {
	return &EVMInterpreter{hostContext: hostContext, evm: evm}
}

func (e *EVMInterpreter) Run(contract *vm.Contract, input []byte, readOnly bool) ([]byte, error) {
	// todo(pdrobnjak): figure out if there is a way to avoid this, probably not, I'll have to replicate every interpreter side effect
	// PASTED FROM GETH
	// Increment the call depth which is restricted to 1024
	e.evm.depth++
	defer func() { e.evm.depth-- }()

	// Make sure the readOnly is only set if we aren't in readOnly yet.
	// This also makes sure that the readOnly flag isn't removed for child calls.
	if readOnly && !e.readOnly {
		e.readOnly = true
		defer func() { e.readOnly = false }()
	}
	// PASTED FROM GETH

	// todo(pdrobnjak): figure out how to access these values and how to validate if they are populated correctly
	callKind := evmc.Call
	recipient := evmc.Address{}
	sender := evmc.Address{}
	static := false
	salt := evmc.Hash{}
	codeAddress := evmc.Address{}
	output, _, _, _, err := e.hostContext.Call(callKind, recipient, sender, contract.Value().Bytes32(), input,
		int64(contract.Gas), e.evm.GetDepth(), static, salt, codeAddress)
	if err != nil {
		return nil, err
	}

	return output, nil
}

func (e *EVMInterpreter) ReadOnly() bool {
	return e.readOnly
}

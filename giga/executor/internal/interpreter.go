package internal

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

	// For CREATE/CREATE2, the initcode is in contract.Code, not in input.
	// For regular calls, input contains the call data.
	codeToExecute := input
	if callOpCode == vm.CREATE || callOpCode == vm.CREATE2 {
		codeToExecute = contract.Code
	}

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

	// Reset SSTORE gas adjustment before execution
	e.hostContext.ResetSstoreGasAdjustment()

	//nolint:gosec // gosec: safe gas conversion
	output, gasLeft, gasRefund, _, err := e.hostContext.Execute(callKind, recipient, sender, contract.Value().Bytes32(), codeToExecute,
		int64(contract.Gas), depth, static)
	if err != nil {
		return nil, err
	}

	// Apply SSTORE gas adjustment for Sei's custom SSTORE cost.
	// evmone uses standard EIP-2200 gas (20k), but Sei may have a different cost.
	// The adjustment is tracked during SetStorage calls and applied here.
	// Adjustment can be positive (charge more) or negative (refund/reduce).
	sstoreAdjustment := e.hostContext.GetSstoreGasAdjustment()
	if sstoreAdjustment != 0 {
		gasLeft -= sstoreAdjustment
		// If gas goes negative, execution would have failed with out of gas
		if gasLeft < 0 {
			return nil, vm.ErrOutOfGas
		}
	}

	// Update the contract's gas to reflect what evmone consumed
	// This is critical for proper gas accounting!
	//nolint:gosec // safe conversion - gasLeft is always <= contract.Gas
	contract.Gas = uint64(gasLeft)

	// Apply gas refund to the EVM's refund counter
	//nolint:gosec // safe conversion
	e.evm.StateDB.AddRefund(uint64(gasRefund))

	return output, nil
}

func (e *EVMInterpreter) ReadOnly() bool {
	return e.readOnly
}

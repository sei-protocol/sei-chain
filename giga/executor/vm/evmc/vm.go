package evmc

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

type VMImpl struct {
	evm *vm.EVM
}

// NewVM creates a new giga executor VM wrapper.
// Note: Currently this wraps geth's EVM. In the future, this will bootstrap evmone
// and use it for execution instead of geth's interpreter.
// TODO(pdrobnjak): populate evmc.VM and integrate evmone for direct bytecode execution
func NewVM(blockCtx vm.BlockContext, stateDB vm.StateDB, chainConfig *params.ChainConfig, config vm.Config, customPrecompiles map[common.Address]vm.PrecompiledContract) *VMImpl {
	evm := vm.NewEVM(blockCtx, stateDB, chainConfig, config, customPrecompiles)
	// Note: We cannot replace geth's interpreter directly since the field is unexported.
	// The evmc integration will need to happen at a different layer or require
	// modifications to go-ethereum to expose the interpreter field.
	// For now, we use geth's default interpreter and the HostContext is prepared
	// for future evmone integration.
	_ = NewHostContext(nil, evm) // Prepared for future use
	return &VMImpl{
		evm: evm,
	}
}

// SetTxContext sets the transaction context for the EVM
func (v *VMImpl) SetTxContext(txCtx vm.TxContext) {
	v.evm.SetTxContext(txCtx)
}

func (v *VMImpl) ApplyMessage(msg *core.Message, gp *core.GasPool) (*core.ExecutionResult, error) {
	executionResult, err := core.ApplyMessage(v.evm, msg, gp)
	return executionResult, err
}

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
// TODO(pdrobnjak): populate evmc.VM and integrate evmone for direct bytecode execution
func NewVM(blockCtx vm.BlockContext, stateDB vm.StateDB, chainConfig *params.ChainConfig, config vm.Config, customPrecompiles map[common.Address]vm.PrecompiledContract) *VMImpl {
	evm := vm.NewEVM(blockCtx, stateDB, chainConfig, config, customPrecompiles)
	hostContext := NewHostContext(nil, evm)
	evm.EVMInterpreter = NewEVMInterpreter(hostContext, evm)
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

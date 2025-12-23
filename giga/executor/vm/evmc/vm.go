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

func NewVM(blockCtx vm.BlockContext, stateDB vm.StateDB, chainConfig *params.ChainConfig, config vm.Config, customPrecompiles map[common.Address]vm.PrecompiledContract) *VMImpl {
	evm := vm.NewEVM(blockCtx, stateDB, chainConfig, config, customPrecompiles)
	// todo(pdrobnjak): populate evmc.VM
	hostContext := NewHostContext(nil, stateDB)
	evm.EVMInterpreter = NewEVMInterpreter(hostContext, evm)
	return &VMImpl{
		evm: evm,
	}
}

// todo(pdrobnjak): we should probably have ExecuteTransaction only that will invoke ApplyMessage and receive a transaction
func (v *VMImpl) ApplyMessage(msg *core.Message, gp *core.GasPool) (*core.ExecutionResult, error) {
	executionResult, err := core.ApplyMessage(v.evm, msg, gp)
	return executionResult, err
}

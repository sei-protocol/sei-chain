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

// this should bootstrap evmone - receive a configuration or something similar, we can do it the same way we did in v3
func NewVM(blockCtx vm.BlockContext, stateDB vm.StateDB, chainConfig *params.ChainConfig, config vm.Config, customPrecompiles map[common.Address]vm.PrecompiledContract) *VMImpl {
	evm := vm.NewEVM(blockCtx, stateDB, chainConfig, config, customPrecompiles)
	// todo(pdrobnjak): populate evmc.VM
	hostContext := NewHostContext(nil, evm)
	evm.EVMInterpreter = NewEVMInterpreter(hostContext, evm)
	return &VMImpl{
		evm: evm,
	}
}

func (v *VMImpl) ApplyMessage(msg *core.Message, gp *core.GasPool) (*core.ExecutionResult, error) {
	executionResult, err := core.ApplyMessage(v.evm, msg, gp)
	return executionResult, err
}

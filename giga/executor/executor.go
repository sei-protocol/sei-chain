package executor

import (
	"math/big"
	"sync"

	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/giga/executor/internal"
)

type Executor struct {
	evm *vm.EVM
}

func NewEvmoneExecutor(evmoneVM *evmc.VM, blockCtx vm.BlockContext, stateDB vm.StateDB, chainConfig *params.ChainConfig, config vm.Config, customPrecompiles map[common.Address]vm.PrecompiledContract) *Executor {
	evm := vm.NewEVM(blockCtx, stateDB, chainConfig, config, customPrecompiles)
	// Pre-compute HostContext config from chain config (avoids per-SSTORE overhead)
	hostConfig := internal.NewHostContextConfig(chainConfig)
	hostContext := internal.NewHostContext(evmoneVM, evm, hostConfig)
	evm.EVMInterpreter = internal.NewEVMInterpreter(hostContext, evm)
	return &Executor{
		evm: evm,
	}
}

func NewGethExecutor(blockCtx vm.BlockContext, stateDB vm.StateDB, chainConfig *params.ChainConfig, config vm.Config, customPrecompiles map[common.Address]vm.PrecompiledContract) *Executor {
	evm := vm.NewEVM(blockCtx, stateDB, chainConfig, config, customPrecompiles)
	return &Executor{
		evm: evm,
	}
}

func (e *Executor) ExecuteTransaction(tx *types.Transaction, sender common.Address, baseFee *big.Int, gasPool *core.GasPool) (*core.ExecutionResult, error) {
	message, err := core.TransactionToMessage(tx, &internal.Signer{From: sender}, baseFee)
	if err != nil {
		return nil, err
	}

	executionResult, err := core.ApplyMessage(e.evm, message, gasPool)
	if err != nil {
		return nil, err
	}

	return executionResult, nil
}

// EVMPool pools vm.EVM instances for reuse within a single block.
// All pooled EVMs share the same block context, chain config, and precompiles.
type EVMPool struct {
	pool sync.Pool
}

// NewEVMPool creates a pool of EVM instances pre-configured for the given block.
func NewEVMPool(blockCtx vm.BlockContext, chainConfig *params.ChainConfig, config vm.Config, customPrecompiles map[common.Address]vm.PrecompiledContract) *EVMPool {
	return &EVMPool{
		pool: sync.Pool{
			New: func() interface{} {
				return vm.NewEVM(blockCtx, nil, chainConfig, config, customPrecompiles)
			},
		},
	}
}

// GetExecutor obtains a pooled EVM, resets it for the given stateDB, and
// returns an Executor wrapping it. Call PutExecutor when done.
func (p *EVMPool) GetExecutor(stateDB vm.StateDB) *Executor {
	evm := p.pool.Get().(*vm.EVM)
	evm.Reset(stateDB)
	return &Executor{evm: evm}
}

// PutExecutor returns the EVM to the pool for reuse.
func (p *EVMPool) PutExecutor(e *Executor) {
	p.pool.Put(e.evm)
}

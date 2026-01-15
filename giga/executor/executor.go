package executor

import (
	"fmt"
	"math/big"
	"os"
	"sync"

	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/giga/executor/internal"
)

const (
	// EvmonePathEnv is the environment variable name for the evmone library path
	EvmonePathEnv = "EVMONE_PATH"
)

var (
	evmoneVM   *evmc.VM
	evmoneOnce sync.Once
	evmoneErr  error
)

// LoadEvmone loads the evmone shared library from the path specified by EVMONE_PATH environment variable.
// It returns the loaded VM instance or an error if loading fails.
// The VM is loaded only once and cached for subsequent calls.
func LoadEvmone() (*evmc.VM, error) {
	evmoneOnce.Do(func() {
		path := os.Getenv(EvmonePathEnv)
		if path == "" {
			evmoneErr = fmt.Errorf("%s environment variable not set", EvmonePathEnv)
			return
		}

		evmoneVM, evmoneErr = evmc.Load(path)
		if evmoneErr != nil {
			evmoneErr = fmt.Errorf("failed to load evmone from %s: %w", path, evmoneErr)
		}
	})
	return evmoneVM, evmoneErr
}

type Executor struct {
	evm *vm.EVM
}

func NewEvmoneExecutor(evmoneVM *evmc.VM, blockCtx vm.BlockContext, stateDB vm.StateDB, chainConfig *params.ChainConfig, config vm.Config, customPrecompiles map[common.Address]vm.PrecompiledContract) *Executor {
	evm := vm.NewEVM(blockCtx, stateDB, chainConfig, config, customPrecompiles)
	hostContext := internal.NewHostContext(evmoneVM, evm)
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

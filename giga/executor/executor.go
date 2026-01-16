package executor

import (
	"math/big"

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
	return e.ExecuteTransactionWithOptions(tx, sender, baseFee, gasPool, false, true)
}

// ExecuteTransactionWithOptions executes a transaction with explicit control over fee and nonce handling.
// - feeCharged: if true, assumes gas fee was already charged (skips BuyGas). Use true to match V2 behavior.
// - shouldIncrementNonce: if true, increments the sender's nonce during execution.
func (e *Executor) ExecuteTransactionWithOptions(tx *types.Transaction, sender common.Address, baseFee *big.Int, gasPool *core.GasPool, feeCharged bool, shouldIncrementNonce bool) (*core.ExecutionResult, error) {
	message, err := core.TransactionToMessage(tx, &internal.Signer{From: sender}, baseFee)
	if err != nil {
		return nil, err
	}

	e.evm.SetTxContext(core.NewEVMTxContext(message))
	executionResult, err := core.NewStateTransition(e.evm, message, gasPool, feeCharged, shouldIncrementNonce).Execute()
	if err != nil {
		return nil, err
	}

	return executionResult, nil
}

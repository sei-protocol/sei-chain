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

// ExecuteTransactionFeeCharged executes a transaction assuming the gas fee has already been charged
// (like V2's msg_server path where the ante handler charges fees separately).
// This ensures the EVM does NOT charge/refund gas fees during execution, matching V2's behavior
// where feeAlreadyCharged=true is passed to StateTransition.Execute().
func (e *Executor) ExecuteTransactionFeeCharged(tx *types.Transaction, sender common.Address, baseFee *big.Int, gasPool *core.GasPool) (*core.ExecutionResult, error) {
	message, err := core.TransactionToMessage(tx, &internal.Signer{From: sender}, baseFee)
	if err != nil {
		return nil, err
	}

	e.evm.SetTxContext(core.NewEVMTxContext(message))
	// feeAlreadyCharged=true: skip buyGas/refund (fees charged separately, like V2 ante handler)
	// shouldIncrementNonce=true: increment nonce during execution (same as V2 msg_server)
	return core.NewStateTransition(e.evm, message, gasPool, true, true).Execute()
}

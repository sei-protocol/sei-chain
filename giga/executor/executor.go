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
	return e.ExecuteTransactionWithOptions(tx, sender, baseFee, gasPool, false, true)
}

// ChargeGasFees deducts gas fees from the sender's balance before execution.
// This should be called before ExecuteTransactionWithOptions with feeCharged=true.
// Returns the core.Message for use in subsequent calls.
func (e *Executor) ChargeGasFees(tx *types.Transaction, sender common.Address, baseFee *big.Int, gasPool *core.GasPool) (*core.Message, error) {
	message, err := core.TransactionToMessage(tx, &internal.Signer{From: sender}, baseFee)
	if err != nil {
		return nil, err
	}

	e.evm.SetTxContext(core.NewEVMTxContext(message))
	// Create StateTransition just to call BuyGas - feeCharged=false so preCheck would call BuyGas,
	// but we call BuyGas directly to have more control
	st := core.NewStateTransition(e.evm, message, gasPool, false, false)

	// Run stateless checks (nonce, signature validity, etc.)
	if err := st.StatelessChecks(); err != nil {
		return nil, err
	}

	// Deduct gas fees from sender's balance
	if err := st.BuyGas(); err != nil {
		return nil, err
	}

	return message, nil
}

// ExecuteWithMessage executes a pre-built message (used after ChargeGasFees).
func (e *Executor) ExecuteWithMessage(message *core.Message, gasPool *core.GasPool, feeCharged bool, shouldIncrementNonce bool) (*core.ExecutionResult, error) {
	e.evm.SetTxContext(core.NewEVMTxContext(message))
	st := core.NewStateTransition(e.evm, message, gasPool, feeCharged, shouldIncrementNonce)
	return st.Execute()
}

// ExecuteTransactionWithOptions executes a transaction with configurable options.
// feeCharged: if true, assumes gas fees were already deducted (skips BuyGas in StateTransition)
// shouldIncrementNonce: if true, increments sender nonce during execution
func (e *Executor) ExecuteTransactionWithOptions(tx *types.Transaction, sender common.Address, baseFee *big.Int, gasPool *core.GasPool, feeCharged bool, shouldIncrementNonce bool) (*core.ExecutionResult, error) {
	message, err := core.TransactionToMessage(tx, &internal.Signer{From: sender}, baseFee)
	if err != nil {
		return nil, err
	}

	e.evm.SetTxContext(core.NewEVMTxContext(message))
	st := core.NewStateTransition(e.evm, message, gasPool, feeCharged, shouldIncrementNonce)
	return st.Execute()
}

package evmonly

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// callTimeout bounds how long a Call may run before its EVM is cancelled,
// matching evmrpc's simulation_evm_timeout default. A var, not a const, so
// tests can shrink it rather than run for the full timeout.
var callTimeout = 60 * time.Second

// Call executes msg as a read-only EVM message call against the current
// committed state and returns the execution result. It persists no state
// change.
func (e *Executor) Call(ctx context.Context, blockCtx BlockContext, msg *core.Message) (*core.ExecutionResult, error) {
	chainConfig := e.chainConfig(blockCtx)
	if err := validateBlockContext(chainConfig, blockCtx); err != nil {
		return nil, err
	}
	if e.stateStore == nil {
		return nil, errMissingStateStore
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	snapshot := e.stateStore.OpenView()
	if snapshot == nil {
		return nil, errors.New("giga store returned a nil snapshot")
	}
	defer snapshot.Close()

	stateDB := e.acquireStateDB(gigaSnapshotStateReader{snapshot: snapshot, missingState: e.missingState})
	defer e.releaseStateDB(stateDB)

	// Gas pricing alone cannot cap wall-clock cost (e.g. modexp with adversarial inputs).
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	// NoBaseFee matches go-ethereum's eth_call: zero fee fields skip the fee-cap check.
	result, cancelled, err := e.applyMessage(callCtx, blockCtx, chainConfig, stateDB, msg, vm.Config{NoBaseFee: true})
	if cancelled {
		return nil, fmt.Errorf("EVM-only call exceeded %s execution timeout", callTimeout)
	}
	if stateErr := stateDB.Error(); stateErr != nil {
		return nil, stateErr
	}
	return result, err
}

// applyMessage executes msg on stateDB under vmConfig and returns its result. It reports whether
// ctx ended during the execution, in which case the result is incomplete.
func (e *Executor) applyMessage(
	ctx context.Context,
	blockCtx BlockContext,
	chainConfig *params.ChainConfig,
	stateDB *nativeStateDB,
	msg *core.Message,
	vmConfig vm.Config,
) (*core.ExecutionResult, bool, error) {
	evm := vm.NewEVM(buildBlockContext(blockCtx), stateDB, chainConfig, vmConfig, e.customContracts)
	stateDB.SetEVM(evm)
	evm.SetTxContext(core.NewEVMTxContext(msg))

	// core.ApplyMessage does not itself respect ctx, so cancel the EVM directly.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-runCtx.Done()
		evm.Cancel()
	}()

	result, err := core.ApplyMessage(evm, msg, new(core.GasPool).AddGas(msg.GasLimit))
	return result, evm.Cancelled(), err
}

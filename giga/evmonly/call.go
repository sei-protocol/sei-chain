package evmonly

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// callTimeout bounds how long a Call may run before its EVM is cancelled,
// matching evmrpc's simulation_evm_timeout default. A var, not a const, so
// tests can shrink it rather than run for the full timeout.
var callTimeout = 60 * time.Second

// Call executes msg as a read-only EVM message call against the current
// committed state and returns the execution result. It persists no state
// change.
func (e *Executor) Call(ctx context.Context, blockCtx BlockContext, msg *core.Message) (*core.ExecutionResult, error) {
	snapshot, err := e.OpenCurrentView(ctx, blockCtx)
	if err != nil {
		return nil, err
	}
	defer snapshot.Close()
	return e.CallAt(ctx, snapshot, blockCtx, msg, CallOverrides{})
}

// CallAt executes msg as a read-only EVM message call against the state view
// holds, with overrides applied, and returns the execution result. It persists
// no state change.
func (e *Executor) CallAt(ctx context.Context, view gigatypes.EVMStateView, blockCtx BlockContext, msg *core.Message, overrides CallOverrides) (*core.ExecutionResult, error) {
	chainConfig := e.chainConfig(blockCtx)
	if err := validateBlockContext(chainConfig, blockCtx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	evmCtx, err := overrides.blockContext(blockCtx)
	if err != nil {
		return nil, err
	}

	stateDB := e.acquireStateDB(gigaSnapshotStateReader{snapshot: view, missingState: e.missingState})
	defer e.releaseStateDB(stateDB)

	// NoBaseFee matches go-ethereum's eth_call: zero fee fields skip the fee-cap check.
	evm := vm.NewEVM(evmCtx, stateDB, chainConfig, vm.Config{NoBaseFee: true}, e.customContracts)
	stateDB.SetEVM(evm)
	if err := overrides.applyState(evm, stateDB, e.customContracts); err != nil {
		return nil, err
	}
	evm.SetTxContext(core.NewEVMTxContext(msg))

	// core.ApplyMessage does not itself respect ctx, so bound it with a timer
	// that cancels the EVM directly; gas pricing alone cannot cap wall-clock
	// cost (e.g. modexp with adversarial inputs).
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	go func() {
		<-callCtx.Done()
		evm.Cancel()
	}()

	gasPool := new(core.GasPool).AddGas(msg.GasLimit)
	result, err := core.ApplyMessage(evm, msg, gasPool)
	if evm.Cancelled() {
		return nil, fmt.Errorf("EVM-only call exceeded %s execution timeout", callTimeout)
	}
	if stateErr := stateDB.Error(); stateErr != nil {
		return nil, stateErr
	}
	return result, err
}

// OpenCurrentView opens the current committed state for a read-only execution
// at blockCtx, once blockCtx and ctx are accepted. The caller closes it.
func (e *Executor) OpenCurrentView(ctx context.Context, blockCtx BlockContext) (gigatypes.StateView, error) {
	if err := validateBlockContext(e.chainConfig(blockCtx), blockCtx); err != nil {
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
	return snapshot, nil
}

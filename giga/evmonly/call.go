package evmonly

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
)

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

	// NoBaseFee lets a caller who leaves GasFeeCap/GasTipCap at zero skip the
	// fee-cap-vs-basefee check, matching go-ethereum's own eth_call behavior.
	evm := vm.NewEVM(buildBlockContext(blockCtx), stateDB, chainConfig, vm.Config{NoBaseFee: true}, customPrecompileMap(e.cfg.CustomPrecompiles))
	stateDB.SetEVM(evm)
	evm.SetTxContext(core.NewEVMTxContext(msg))

	gasPool := new(core.GasPool).AddGas(msg.GasLimit)
	result, err := core.ApplyMessage(evm, msg, gasPool)
	if stateErr := stateDB.Error(); stateErr != nil {
		return nil, stateErr
	}
	return result, err
}

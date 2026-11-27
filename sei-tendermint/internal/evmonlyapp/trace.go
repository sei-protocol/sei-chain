package evmonlyapp

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
)

var errTraceGenesis = errors.New("block 0 has no parent state to trace against")

// EvmBlockContext returns the EVM execution context of block.
func (a *evmOnlyApplication) EvmBlockContext(block abevm.BlockRef) (vm.BlockContext, error) {
	number, err := blockNumber(block)
	if err != nil {
		return vm.BlockContext{}, err
	}
	return evmonly.EVMBlockContext(a.traceBlockContext(number, block)), nil
}

// EvmStateAt returns the committed state after height, or the latest committed state for
// rpc.LatestBlockNumber. The caller must close the returned handle.
func (a *evmOnlyApplication) EvmStateAt(ctx context.Context, height int64) (*abevm.StateHandle, error) {
	return a.openTraceState(height, func(executor *evmonly.Executor, view gigatypes.StateView) (vm.SeiStateDB, error) {
		return executor.ViewState(view), nil
	})
}

// EvmStateBeforeTx returns the state before transaction len(txs) in block, after replaying txs.
// The caller must close the returned handle.
func (a *evmOnlyApplication) EvmStateBeforeTx(ctx context.Context, block abevm.BlockRef, txs [][]byte) (*abevm.StateHandle, error) {
	number, err := blockNumber(block)
	if err != nil {
		return nil, err
	}
	if number == 0 {
		return nil, errTraceGenesis
	}
	parent := int64(number - 1) //nolint:gosec // G115: blockNumber bounds number to int64.
	req := evmonly.BlockRequest{Context: a.traceBlockContext(number, block), Txs: txs}
	return a.openTraceState(parent, func(executor *evmonly.Executor, view gigatypes.StateView) (vm.SeiStateDB, error) {
		return executor.ReplayState(ctx, view, req)
	})
}

// traceBlockContext returns the context block executed at, at its base fee. The block hash, which
// only stamps receipts, is left zero.
func (a *evmOnlyApplication) traceBlockContext(number uint64, block abevm.BlockRef) evmonly.BlockContext {
	// The gas limit is fixed at InitChain, so the committed one is every block's.
	return a.blockContext(number, block.Time, a.EvmGasLimit(), block.BaseFee, block.ParentHash, common.Hash{})
}

// openTraceState opens the committed EVM state after height, or the latest committed state when
// height is rpc.LatestBlockNumber, and returns the state build makes of it. A read the state store
// can no longer serve, such as one at a height pruned mid-replay, is returned as an error.
func (a *evmOnlyApplication) openTraceState(height int64, build func(*evmonly.Executor, gigatypes.StateView) (vm.SeiStateDB, error)) (_ *abevm.StateHandle, err error) {
	executor, err := a.loadedExecutor("trace")
	if err != nil {
		return nil, err
	}
	view, err := a.traceView(height)
	if err != nil {
		return nil, err
	}
	defer func() {
		if r := recover(); r != nil {
			view.Close()
			err = fmt.Errorf("replay the EVM state after block %d: %v", view.GetBlockHeight(), r)
		}
	}()
	stateDB, err := build(executor, view)
	if err != nil {
		view.Close()
		return nil, err
	}
	return abevm.NewStateHandle(stateDB, view.Close), nil
}

// traceView opens the committed EVM state after height, or the latest committed state when height is
// rpc.LatestBlockNumber.
func (a *evmOnlyApplication) traceView(height int64) (gigatypes.StateView, error) {
	if height == rpc.LatestBlockNumber.Int64() {
		return a.openSettledView(), nil
	}
	return a.evmStateAt(height)
}

// loadedExecutor returns the executor, which exists once the chain is initialized. action names the
// caller for its error message.
func (a *evmOnlyApplication) loadedExecutor(action string) (*evmonly.Executor, error) {
	for exec := range a.executor.Lock() {
		executor, ok := exec.Get()
		if !ok {
			return nil, fmt.Errorf("EVM-only %s attempted before InitChain", action)
		}
		return executor, nil
	}
	panic("unreachable")
}

// blockNumber returns the block height, which must fit in an int64.
func blockNumber(block abevm.BlockRef) (uint64, error) {
	if uint64(block.Height) > math.MaxInt64 {
		return 0, fmt.Errorf("block number %d is out of range", block.Height)
	}
	return uint64(block.Height), nil
}

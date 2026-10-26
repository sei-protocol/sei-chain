package evmonlyapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

var errTraceGenesis = errors.New("block 0 has no parent state to trace against")

// EvmBlockContext returns the go-ethereum block context this application executed header's block
// at. Only header's number, time and parent hash are read.
func (a *evmOnlyApplication) EvmBlockContext(header *ethtypes.Header) (vm.BlockContext, error) {
	number, err := headerNumber(header)
	if err != nil {
		return vm.BlockContext{}, err
	}
	return evmonly.EVMBlockContext(a.traceBlockContext(number, header)), nil
}

// EvmState returns the latest committed EVM state, held in memory, and the function that releases
// it. The state must not be used once released.
func (a *evmOnlyApplication) EvmState(ctx context.Context) (vm.StateDB, func(), error) {
	executor, err := a.loadedExecutor("trace")
	if err != nil {
		return nil, nil, err
	}
	return replayOnView(ctx, executor, a.openSettledView(), evmonly.BlockRequest{})
}

// EvmStateAt returns the committed EVM state after height, held in memory, and the function that
// releases it. The state must not be used once released.
func (a *evmOnlyApplication) EvmStateAt(ctx context.Context, height int64) (vm.StateDB, func(), error) {
	return a.replayState(ctx, height, evmonly.BlockRequest{})
}

// EvmStateBeforeTx returns the EVM state the transaction at index len(txs) of header's block
// executed against, and the function that releases it: the committed state after the previous
// block with txs, the block's transactions before it, applied. The state must not be used once
// released.
func (a *evmOnlyApplication) EvmStateBeforeTx(ctx context.Context, header *ethtypes.Header, txs [][]byte) (vm.StateDB, func(), error) {
	number, err := headerNumber(header)
	if err != nil {
		return nil, nil, err
	}
	if number == 0 {
		return nil, nil, errTraceGenesis
	}
	parent := int64(number - 1) //nolint:gosec // G115: headerNumber bounds number to int64.
	return a.replayState(ctx, parent, evmonly.BlockRequest{Context: a.traceBlockContext(number, header), Txs: txs})
}

// traceBlockContext returns the context header's block number executed at. The block hash, which
// only stamps receipts, is left zero.
func (a *evmOnlyApplication) traceBlockContext(number uint64, header *ethtypes.Header) evmonly.BlockContext {
	// The gas limit is fixed at InitChain, so the committed one is every block's.
	return a.blockContext(number, header.Time, a.EvmGasLimit(), header.ParentHash, common.Hash{})
}

// replayState opens the committed EVM state after height and applies req's transactions to it.
func (a *evmOnlyApplication) replayState(ctx context.Context, height int64, req evmonly.BlockRequest) (vm.StateDB, func(), error) {
	executor, err := a.loadedExecutor("trace")
	if err != nil {
		return nil, nil, err
	}
	view, err := a.evmStateAt(height)
	if err != nil {
		return nil, nil, err
	}
	return replayOnView(ctx, executor, view, req)
}

// replayOnView applies req's transactions to view with executor, returning the result and the
// function that closes view; view is closed on failure. A read the state store can no longer serve,
// such as one at a height pruned mid-replay, is returned as an error.
func replayOnView(ctx context.Context, executor *evmonly.Executor, view gigatypes.StateView, req evmonly.BlockRequest) (_ vm.StateDB, _ func(), err error) {
	defer func() {
		if r := recover(); r != nil {
			view.Close()
			err = fmt.Errorf("replay the EVM state after block %d: %v", view.GetBlockHeight(), r)
		}
	}()
	stateDB, err := executor.ReplayState(ctx, view, req)
	if err != nil {
		view.Close()
		return nil, nil, err
	}
	return stateDB, view.Close, nil
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

// headerNumber returns header's block number, which must fit in an int64.
func headerNumber(header *ethtypes.Header) (uint64, error) {
	if !header.Number.IsInt64() || header.Number.Sign() < 0 {
		return 0, fmt.Errorf("block number %s is out of range", header.Number)
	}
	return header.Number.Uint64(), nil
}

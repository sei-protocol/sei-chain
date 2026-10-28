package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/export"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
)

type estimateAPI struct {
	backend Backend
	// maxCalls caps the calls one eth_estimateGasAfterCalls request may apply; zero disables the
	// cap.
	maxCalls int
}

// EstimateGas returns a gas limit that lets args execute without running out
// of gas against the current committed state. Like eth_call it persists no
// state change; a call that reverts even at the highest allowed gas returns
// the revert error.
func (api *estimateAPI) EstimateGas(ctx context.Context, args export.TransactionArgs, block *ethrpc.BlockNumberOrHash) (hexutil.Uint64, error) {
	return api.EstimateGasAfterCalls(ctx, args, nil, block)
}

// EstimateGasAfterCalls returns what eth_estimateGas returns for args against the current
// committed state with calls applied to it first, in order.
func (api *estimateAPI) EstimateGasAfterCalls(ctx context.Context, args export.TransactionArgs, calls []export.TransactionArgs, block *ethrpc.BlockNumberOrHash) (hexutil.Uint64, error) {
	if api.maxCalls > 0 && len(calls) > api.maxCalls {
		return 0, fmt.Errorf("eth_estimateGasAfterCalls: too many calls (%d > %d)", len(calls), api.maxCalls)
	}
	selector := ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber)
	if block != nil {
		selector = *block
	}
	if err := requireCurrentState(selector); err != nil {
		return 0, err
	}
	baseFee, err := api.backend.EvmBaseFee()
	if err != nil {
		return 0, err
	}
	chainID := new(big.Int).SetUint64(api.backend.EvmChainID())
	callMsgs := make([]*core.Message, len(calls))
	for i := range calls {
		if err := calls[i].CallDefaults(defaultCallGasCap, baseFee, chainID); err != nil {
			return 0, fmt.Errorf("call %d: %w", i, err)
		}
		callMsgs[i] = calls[i].ToMessage(baseFee, true, true)
	}
	// A zero (not nil) Gas leaves an omitted limit to default to the block gas
	// limit downstream, rather than the fixed defaultCallGasCap.
	if args.Gas == nil {
		args.Gas = new(hexutil.Uint64)
	}
	if err := args.CallDefaults(defaultCallGasCap, baseFee, chainID); err != nil {
		return 0, err
	}
	msg := args.ToMessage(baseFee, true, true)

	estimate, revert, err := api.estimate(ctx, callMsgs, msg)
	if err != nil {
		if errors.Is(err, vm.ErrExecutionReverted) {
			return 0, newRevertError(revert)
		}
		return 0, err
	}
	return hexutil.Uint64(estimate), nil
}

// estimate searches msg's gas against the current committed state, with calls applied first when
// there are any.
func (api *estimateAPI) estimate(ctx context.Context, calls []*core.Message, msg *core.Message) (uint64, []byte, error) {
	if len(calls) == 0 {
		return api.backend.EvmEstimateGas(ctx, msg, defaultCallGasCap)
	}
	return api.backend.EvmEstimateGasAfterCalls(ctx, calls, msg, defaultCallGasCap)
}

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
	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

type estimateAPI struct {
	backend Backend
	blocks  *blockAPI
	limits  overrideLimits
	// maxCalls caps the calls one eth_estimateGasAfterCalls request may apply; zero disables the
	// cap.
	maxCalls int
}

// EstimateGas returns a gas limit that lets args execute without running out
// of gas against the committed state after block, the latest by default, with
// the state and block overrides applied. Like eth_call it persists no state
// change; a call that reverts even at the highest allowed gas returns the
// revert error.
func (api *estimateAPI) EstimateGas(ctx context.Context, args export.TransactionArgs, block *ethrpc.BlockNumberOrHash, stateOverrides *export.StateOverride, blockOverrides *export.BlockOverrides) (hexutil.Uint64, error) {
	overrides, err := api.limits.callOverrides(stateOverrides, blockOverrides)
	if err != nil {
		return 0, err
	}
	return api.estimateAfterCalls(ctx, args, nil, block, overrides)
}

// EstimateGasAfterCalls returns what eth_estimateGas returns for args against the committed state
// after block, the latest by default, with the state override applied and then calls applied, in
// order.
func (api *estimateAPI) EstimateGasAfterCalls(ctx context.Context, args export.TransactionArgs, calls []export.TransactionArgs, block *ethrpc.BlockNumberOrHash, stateOverrides *export.StateOverride) (hexutil.Uint64, error) {
	if api.maxCalls > 0 && len(calls) > api.maxCalls {
		return 0, fmt.Errorf("eth_estimateGasAfterCalls: too many calls (%d > %d)", len(calls), api.maxCalls)
	}
	overrides, err := api.limits.callOverrides(stateOverrides, nil)
	if err != nil {
		return 0, err
	}
	return api.estimateAfterCalls(ctx, args, calls, block, overrides)
}

// estimateAfterCalls returns the gas args needs against the committed state after block, the
// latest by default, with overrides applied and then calls applied, in order.
func (api *estimateAPI) estimateAfterCalls(
	ctx context.Context,
	args export.TransactionArgs,
	calls []export.TransactionArgs,
	block *ethrpc.BlockNumberOrHash,
	overrides evmonly.CallOverrides,
) (hexutil.Uint64, error) {
	selector := ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber)
	if block != nil {
		selector = *block
	}
	height, latest, err := stateHeight(ctx, api.backend, selector)
	if err != nil {
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
		callMsgs[i] = calls[i].ToMessage(baseFee, true)
	}
	// A zero (not nil) Gas leaves an omitted limit to default to the block gas
	// limit downstream, rather than the fixed defaultCallGasCap.
	if args.Gas == nil {
		args.Gas = new(hexutil.Uint64)
	}
	if err := args.CallDefaults(defaultCallGasCap, baseFee, chainID); err != nil {
		return 0, err
	}
	msg := args.ToMessage(baseFee, true)

	estimate, revert, err := api.estimate(ctx, callMsgs, msg, height, latest, overrides)
	if err != nil {
		if errors.Is(err, vm.ErrExecutionReverted) {
			return 0, newRevertError(revert)
		}
		return 0, err
	}
	return hexutil.Uint64(estimate), nil
}

// estimate searches msg's gas with overrides and then calls applied, against the latest committed
// state, or else against the state after the block at height.
func (api *estimateAPI) estimate(ctx context.Context, calls []*core.Message, msg *core.Message, height int64, latest bool, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	if latest {
		if len(calls) == 0 {
			return api.backend.EvmEstimateGas(ctx, msg, defaultCallGasCap, overrides)
		}
		return api.backend.EvmEstimateGasAfterCalls(ctx, calls, msg, defaultCallGasCap, overrides)
	}
	header, err := api.blocks.executionHeaderAt(ctx, height)
	if err != nil {
		return 0, nil, err
	}
	if len(calls) == 0 {
		return api.backend.EvmEstimateGasAt(ctx, msg, defaultCallGasCap, header, overrides)
	}
	return api.backend.EvmEstimateGasAfterCallsAt(ctx, calls, msg, defaultCallGasCap, header, overrides)
}

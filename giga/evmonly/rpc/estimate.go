package rpc

import (
	"context"
	"errors"
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
	// A zero (not nil) Gas leaves an omitted limit to default to the block gas
	// limit downstream, rather than the fixed defaultCallGasCap.
	if args.Gas == nil {
		args.Gas = new(hexutil.Uint64)
	}
	if err := args.CallDefaults(defaultCallGasCap, baseFee, chainID); err != nil {
		return 0, err
	}
	msg := args.ToMessage(baseFee, true, true)

	estimate, revert, err := api.estimate(ctx, msg, height, latest, overrides)
	if err != nil {
		if errors.Is(err, vm.ErrExecutionReverted) {
			return 0, newRevertError(revert)
		}
		return 0, err
	}
	return hexutil.Uint64(estimate), nil
}

// estimate searches msg's gas with overrides against the latest committed
// state, or else against the state after the block at height.
func (api *estimateAPI) estimate(ctx context.Context, msg *core.Message, height int64, latest bool, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	if latest {
		return api.backend.EvmEstimateGas(ctx, msg, defaultCallGasCap, overrides)
	}
	header, err := api.blocks.executionHeaderAt(ctx, height)
	if err != nil {
		return 0, nil, err
	}
	return api.backend.EvmEstimateGasAt(ctx, msg, defaultCallGasCap, header, overrides)
}

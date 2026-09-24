package rpc

import (
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/export"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
)

// EstimateGas returns a gas limit that lets args execute without running out
// of gas against the current committed state. Like eth_call it creates no
// transaction and persists no state change; a call that reverts even at the
// highest allowed gas returns the revert error rather than an estimate.
//
// TODO: Support geth block overrides for number, difficulty, time, gasLimit,
// feeRecipient, prevRandao, baseFeePerGas, and blobBaseFee.
func (api *callAPI) EstimateGas(ctx context.Context, args export.TransactionArgs, block *ethrpc.BlockNumberOrHash) (hexutil.Uint64, error) {
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
	// A zero gas keeps CallDefaults from filling in the cap, so an omitted gas
	// falls through to the block gas limit in the estimator.
	if args.Gas == nil {
		args.Gas = new(hexutil.Uint64)
	}
	if err := args.CallDefaults(defaultCallGasCap, baseFee, chainID); err != nil {
		return 0, err
	}
	msg := args.ToMessage(baseFee, true, true)

	estimate, revert, err := api.backend.EvmEstimateGas(ctx, msg, defaultCallGasCap)
	if err != nil {
		if errors.Is(err, vm.ErrExecutionReverted) {
			return 0, newRevertError(revert)
		}
		return 0, err
	}
	return hexutil.Uint64(estimate), nil
}

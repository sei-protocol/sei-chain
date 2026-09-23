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
	"github.com/ethereum/go-ethereum/params"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
)

// estimateGasErrorRatio is the relative gap between the highest failing and
// lowest passing gas limit at which the search stops, matching go-ethereum's
// gasestimator. A 1.5% tolerance trades a few EVM executions for a result
// that is at most that much above the true minimum.
const estimateGasErrorRatio = 0.015

// EstimateGas returns a gas limit that lets args execute without running out
// of gas against the current committed state. Like eth_call it creates no
// transaction and persists no state change; a call that reverts even at the
// highest allowed gas returns the revert error rather than an estimate.
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
	if err := args.CallDefaults(defaultCallGasCap, baseFee, chainID); err != nil {
		return 0, err
	}
	msg := args.ToMessage(baseFee, true, true)

	hi, err := api.estimateUpperBound(msg)
	if err != nil {
		return 0, err
	}
	estimate, err := api.searchGasLimit(ctx, msg, hi)
	if err != nil {
		return 0, err
	}
	return hexutil.Uint64(estimate), nil
}

// estimateUpperBound returns the highest gas limit the search may try: the
// caller's explicit limit or the block gas limit, lowered to what the sender's
// balance can pay for when the message carries a non-zero fee cap, and never
// above defaultCallGasCap.
func (api *callAPI) estimateUpperBound(msg *core.Message) (uint64, error) {
	hi := msg.GasLimit
	if hi < params.TxGas {
		blockGasLimit, err := api.backend.EvmGasLimit()
		if err != nil {
			return 0, err
		}
		hi = blockGasLimit
	}
	feeCap := msg.GasFeeCap
	if feeCap == nil || feeCap.Sign() == 0 {
		feeCap = msg.GasPrice
	}
	if feeCap != nil && feeCap.Sign() > 0 {
		balance := api.backend.EvmBalance(msg.From)
		available := balance.ToBig()
		if msg.Value != nil {
			if msg.Value.Cmp(available) >= 0 {
				return 0, core.ErrInsufficientFundsForTransfer
			}
			available.Sub(available, msg.Value)
		}
		allowance := new(big.Int).Div(available, feeCap)
		if allowance.IsUint64() && allowance.Uint64() < hi {
			hi = allowance.Uint64()
		}
	}
	if hi > defaultCallGasCap {
		hi = defaultCallGasCap
	}
	return hi, nil
}

// searchGasLimit finds the lowest gas limit in (params.TxGas-1, hi] at which
// msg succeeds, to within estimateGasErrorRatio. It executes at hi first so a
// message that cannot succeed at all fails fast with its revert reason, then
// narrows from an optimistic guess derived from that run's gas usage.
func (api *callAPI) searchGasLimit(ctx context.Context, msg *core.Message, hi uint64) (uint64, error) {
	lo := params.TxGas - 1
	failed, result, err := api.executeWithGas(ctx, msg, hi)
	if err != nil {
		return 0, err
	}
	if failed {
		if len(result.Revert()) > 0 {
			return 0, newRevertError(result)
		}
		if errors.Is(result.Err, vm.ErrOutOfGas) || errors.Is(result.Err, core.ErrIntrinsicGas) {
			return 0, fmt.Errorf("gas required exceeds allowance (%d)", hi)
		}
		return 0, result.Err
	}
	// The gas needed to run the top-level frame is at least what was used plus
	// what was refunded; the 63/64 rule and the stipend cover the call overhead.
	optimistic := (result.UsedGas + result.RefundedGas + params.CallStipend) * 64 / 63
	if optimistic < hi {
		failed, _, err := api.executeWithGas(ctx, msg, optimistic)
		if err != nil {
			return 0, err
		}
		if failed {
			lo = optimistic
		} else {
			hi = optimistic
		}
	}
	for lo+1 < hi {
		if float64(hi-lo)/float64(hi) < estimateGasErrorRatio {
			break
		}
		mid := (hi + lo) / 2
		if mid > lo*2 {
			// Most transactions need far less than the block gas limit; growing
			// lo geometrically converges faster than bisecting from the top.
			mid = lo * 2
		}
		failed, _, err := api.executeWithGas(ctx, msg, mid)
		if err != nil {
			return 0, err
		}
		if failed {
			lo = mid
		} else {
			hi = mid
		}
	}
	return hi, nil
}

// executeWithGas runs msg with gas as its limit and reports whether the
// execution failed for a reason a higher gas limit could fix. An error from
// the backend itself, rather than from the EVM, is returned as err.
func (api *callAPI) executeWithGas(ctx context.Context, msg *core.Message, gas uint64) (failed bool, result *core.ExecutionResult, err error) {
	attempt := *msg
	attempt.GasLimit = gas
	result, err = api.backend.EvmCall(ctx, &attempt)
	if err != nil {
		if errors.Is(err, core.ErrIntrinsicGas) || errors.Is(err, core.ErrFloorDataGas) {
			return true, &core.ExecutionResult{Err: err}, nil
		}
		return false, nil, err
	}
	if result == nil {
		return false, nil, errors.New("EVM-only call returned no result")
	}
	return result.Failed(), result, nil
}

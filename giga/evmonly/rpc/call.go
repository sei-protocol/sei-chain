package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/export"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

// defaultCallGasCap bounds the gas an eth_call may consume, filling in an
// omitted gas limit and capping a caller-supplied one. It matches evmrpc's
// simulation_gas_limit default.
const defaultCallGasCap = 10_000_000

type callAPI struct {
	backend Backend
	blocks  *blockAPI
	limits  overrideLimits
}

// Call executes args as a read-only EVM message call against the committed
// state after block, in the context that block executed at, with the state and
// block overrides applied, and returns the return data. A latest tag runs
// against the latest committed state. It creates no transaction and persists
// no state change.
func (api *callAPI) Call(ctx context.Context, args export.TransactionArgs, block ethrpc.BlockNumberOrHash, stateOverrides *export.StateOverride, blockOverrides *export.BlockOverrides) (hexutil.Bytes, error) {
	overrides, err := api.limits.callOverrides(stateOverrides, blockOverrides)
	if err != nil {
		return nil, err
	}
	height, latest, err := stateHeight(ctx, api.backend, block)
	if err != nil {
		return nil, err
	}
	baseFee, err := api.backend.EvmBaseFee()
	if err != nil {
		return nil, err
	}
	chainID := new(big.Int).SetUint64(api.backend.EvmChainID())
	if err := args.CallDefaults(defaultCallGasCap, baseFee, chainID); err != nil {
		return nil, err
	}
	msg := args.ToMessage(baseFee, true, true)

	result, err := api.execute(ctx, msg, height, latest, overrides)
	if err != nil {
		return nil, err
	}
	if len(result.Revert()) > 0 {
		return nil, newRevertError(result.Revert())
	}
	if result.Err != nil {
		return nil, result.Err
	}
	return result.Return(), nil
}

// execute runs msg with overrides against the latest committed state, or else
// against the state after the block at height.
func (api *callAPI) execute(ctx context.Context, msg *core.Message, height int64, latest bool, overrides evmonly.CallOverrides) (*core.ExecutionResult, error) {
	if latest {
		return api.backend.EvmCall(ctx, msg, overrides)
	}
	header, err := api.blocks.executionHeaderAt(ctx, height)
	if err != nil {
		return nil, err
	}
	return api.backend.EvmCallAt(ctx, msg, header, overrides)
}

// newRevertError builds the JSON-RPC error for a reverted call: code 3 with
// the raw revert data and, when decodable, the reason in the message.
func newRevertError(revert []byte) *revertError {
	reason, errUnpack := abi.UnpackRevert(revert)
	err := errors.New("execution reverted")
	if errUnpack == nil {
		err = fmt.Errorf("execution reverted: %v", reason)
	}
	return &revertError{error: err, reason: hexutil.Encode(revert)}
}

// revertError is a JSON-RPC error carrying an EVM revert reason.
type revertError struct {
	error
	reason string // revert reason, hex encoded
}

// ErrorCode returns the JSON-RPC error code for a revert.
// See: https://github.com/ethereum/wiki/wiki/JSON-RPC-Error-Codes-Improvement-Proposal
func (e *revertError) ErrorCode() int {
	return 3
}

// ErrorData returns the hex encoded revert reason.
func (e *revertError) ErrorData() any {
	return e.reason
}

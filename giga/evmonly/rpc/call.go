package rpc

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/export"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/sei-protocol/sei-chain/giga/evmonly/rpc/rpcerr"
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
)

// defaultCallGasCap bounds the gas an eth_call may consume, filling in an
// omitted gas limit and capping a caller-supplied one. It matches evmrpc's
// simulation_gas_limit default.
const defaultCallGasCap = 10_000_000

type callAPI struct {
	backend Backend
	store   receiptdb.ReceiptStore
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
	height, latest, err := stateHeight(ctx, api.backend, api.store, block)
	if err != nil {
		return nil, err
	}
	baseFee := overrides.BaseFee(api.backend.EvmBaseFee())
	chainID := new(big.Int).SetUint64(api.backend.EvmChainID())
	if err := args.CallDefaults(defaultCallGasCap, baseFee, chainID); err != nil {
		return nil, err
	}
	msg := args.ToMessage(baseFee, true)

	result, err := api.execute(ctx, msg, height, latest, overrides)
	if err != nil {
		return nil, err
	}
	if len(result.Revert()) > 0 {
		return nil, rpcerr.ExecutionReverted(result.Revert())
	}
	if result.Err != nil {
		return nil, result.Err
	}
	return result.Return(), nil
}

// execute runs msg with overrides against the latest committed state, or else
// against the state after the block at height.
func (api *callAPI) execute(ctx context.Context, msg *core.Message, height int64, latest bool, overrides abevm.CallOverrides) (*core.ExecutionResult, error) {
	if latest {
		return api.backend.EvmCall(ctx, msg, overrides)
	}
	header, err := api.blocks.executionHeaderAt(ctx, height)
	if err != nil {
		return nil, err
	}
	return api.backend.EvmCallAt(ctx, msg, header, overrides)
}

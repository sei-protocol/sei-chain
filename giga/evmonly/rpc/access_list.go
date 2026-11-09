package rpc

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/export"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
)

// accessListResult is an eth_createAccessList response.
type accessListResult struct {
	AccessList *ethtypes.AccessList `json:"accessList"`
	Error      string               `json:"error,omitempty"`
	GasUsed    hexutil.Uint64       `json:"gasUsed"`
}

// CreateAccessList returns the access list args touches against the committed state after block,
// the latest by default, in the context that block executed at, the gas it uses with that list,
// and the error its execution ends with, if any. It creates no transaction and persists no state
// change.
func (api *callAPI) CreateAccessList(ctx context.Context, args export.TransactionArgs, block *ethrpc.BlockNumberOrHash) (*accessListResult, error) {
	selector := ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber)
	if block != nil {
		selector = *block
	}
	height, latest, err := stateHeight(ctx, api.backend, selector)
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
	accessList, result, err := api.createAccessList(ctx, args.ToMessage(baseFee, true, true), height, latest)
	if err != nil {
		return nil, err
	}
	response := &accessListResult{AccessList: &accessList, GasUsed: hexutil.Uint64(result.UsedGas)}
	if result.Err != nil {
		response.Error = result.Err.Error()
	}
	return response, nil
}

// createAccessList builds msg's access list against the latest committed state, or else against
// the state after the block at height.
func (api *callAPI) createAccessList(ctx context.Context, msg *core.Message, height int64, latest bool) (ethtypes.AccessList, *core.ExecutionResult, error) {
	if latest {
		return api.backend.EvmCreateAccessList(ctx, msg)
	}
	header, err := api.blocks.executionHeaderAt(ctx, height)
	if err != nil {
		return nil, nil, err
	}
	return api.backend.EvmCreateAccessListAt(ctx, msg, header)
}

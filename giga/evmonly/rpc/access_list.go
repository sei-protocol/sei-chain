package rpc

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common/hexutil"
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

// CreateAccessList returns the access list args touches against the current committed state,
// the gas it uses with that list, and the error its execution ends with, if any. It creates no
// transaction and persists no state change.
func (api *callAPI) CreateAccessList(ctx context.Context, args export.TransactionArgs, block *ethrpc.BlockNumberOrHash) (*accessListResult, error) {
	selector := ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber)
	if block != nil {
		selector = *block
	}
	if err := requireCurrentState(selector); err != nil {
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
	accessList, result, err := api.backend.EvmCreateAccessList(ctx, args.ToMessage(baseFee, true, true))
	if err != nil {
		return nil, err
	}
	response := &accessListResult{AccessList: &accessList, GasUsed: hexutil.Uint64(result.UsedGas)}
	if result.Err != nil {
		response.Error = result.Err.Error()
	}
	return response, nil
}

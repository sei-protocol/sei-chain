package keeper

import (
	"context"
	"encoding/hex"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/sei-protocol/sei-chain/x/nitro/types"
)

var _ types.QueryServer = Keeper{}

func (k Keeper) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params := k.GetParams(sdkCtx)

	return &types.QueryParamsResponse{Params: params}, nil
}

func (k Keeper) RecordedTransactionData(ctx context.Context, req *types.QueryRecordedTransactionDataRequest) (*types.QueryRecordedTransactionDataResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	txs, err := k.GetTransactionData(sdkCtx, req.Slot)
	if err != nil {
		return nil, err
	}

	hexTxs := []string{}
	for _, tx := range txs {
		hexTxs = append(hexTxs, hex.EncodeToString(tx))
	}
	return &types.QueryRecordedTransactionDataResponse{Txs: hexTxs}, nil
}

func (k Keeper) StateRoot(ctx context.Context, req *types.QueryStateRootRequest) (*types.QueryStateRootResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	root, err := k.GetStateRoot(sdkCtx, req.Slot)
	if err != nil {
		return nil, err
	}

	return &types.QueryStateRootResponse{Root: hex.EncodeToString(root)}, nil
}

func (k Keeper) Sender(ctx context.Context, req *types.QuerySenderRequest) (*types.QuerySenderResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sender, exists := k.GetSender(sdkCtx, req.Slot)
	if !exists {
		return nil, sdkerrors.ErrKeyNotFound
	}
	return &types.QuerySenderResponse{Sender: sender}, nil
}

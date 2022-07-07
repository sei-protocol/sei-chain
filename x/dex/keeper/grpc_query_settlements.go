package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k Keeper) SettlementsAll(c context.Context, req *types.QueryAllSettlementsRequest) (*types.QueryAllSettlementsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var settlements []types.Settlements
	ctx := sdk.UnwrapSDKContext(c)

	store := ctx.KVStore(k.storeKey)
	settlementStore := prefix.NewStore(store, types.KeyPrefix(types.SettlementEntryKey))

	pageRes, err := query.Paginate(settlementStore, req.Pagination, func(key []byte, value []byte) error {
		var settlement types.Settlements
		if err := k.Cdc.Unmarshal(value, &settlement); err != nil {
			return err
		}

		settlements = append(settlements, settlement)
		return nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllSettlementsResponse{Settlements: settlements, Pagination: pageRes}, nil
}

func (k Keeper) Settlements(c context.Context, req *types.QueryGetSettlementsRequest) (*types.QueryGetSettlementsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)
	Settlements, found := k.GetSettlements(ctx, req.ContractAddr, req.BlockHeight, types.GetContractDenomName(req.PriceDenom), types.GetContractDenomName(req.AssetDenom))
	if !found {
		return nil, sdkerrors.ErrKeyNotFound
	}

	return &types.QueryGetSettlementsResponse{Settlements: Settlements}, nil
}

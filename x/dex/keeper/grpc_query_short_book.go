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

func (k Keeper) ShortBookAll(c context.Context, req *types.QueryAllShortBookRequest) (*types.QueryAllShortBookResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var shortBooks []types.ShortBook
	ctx := sdk.UnwrapSDKContext(c)

	store := ctx.KVStore(k.storeKey)
	shortBookStore := prefix.NewStore(store, types.KeyPrefix(types.ShortBookKey))

	pageRes, err := query.Paginate(shortBookStore, req.Pagination, func(key []byte, value []byte) error {
		var shortBook types.ShortBook
		if err := k.cdc.Unmarshal(value, &shortBook); err != nil {
			return err
		}

		shortBooks = append(shortBooks, shortBook)
		return nil
	})

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllShortBookResponse{ShortBook: shortBooks, Pagination: pageRes}, nil
}

func (k Keeper) ShortBook(c context.Context, req *types.QueryGetShortBookRequest) (*types.QueryGetShortBookResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)
	shortBook, found := k.GetShortBookByPrice(ctx, req.ContractAddr, req.Id, req.PriceDenom, req.AssetDenom)
	if !found {
		return nil, sdkerrors.ErrKeyNotFound
	}

	return &types.QueryGetShortBookResponse{ShortBook: shortBook}, nil
}

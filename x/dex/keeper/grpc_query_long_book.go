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

func (k Keeper) LongBookAll(c context.Context, req *types.QueryAllLongBookRequest) (*types.QueryAllLongBookResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var longBooks []types.LongBook
	ctx := sdk.UnwrapSDKContext(c)

	store := ctx.KVStore(k.storeKey)
	longBookStore := prefix.NewStore(store, types.KeyPrefix(types.LongBookKey))

	pageRes, err := query.Paginate(longBookStore, req.Pagination, func(key []byte, value []byte) error {
		var longBook types.LongBook
		if err := k.cdc.Unmarshal(value, &longBook); err != nil {
			return err
		}

		longBooks = append(longBooks, longBook)
		return nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllLongBookResponse{LongBook: longBooks, Pagination: pageRes}, nil
}

func (k Keeper) LongBook(c context.Context, req *types.QueryGetLongBookRequest) (*types.QueryGetLongBookResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)
	longBook, found := k.GetLongBookByPrice(ctx, req.ContractAddr, req.Id, req.PriceDenom, req.AssetDenom)
	if !found {
		return nil, sdkerrors.ErrKeyNotFound
	}

	return &types.QueryGetLongBookResponse{LongBook: longBook}, nil
}

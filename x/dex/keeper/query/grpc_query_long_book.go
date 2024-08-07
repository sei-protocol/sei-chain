package query

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k KeeperWrapper) LongBookAll(c context.Context, req *types.QueryAllLongBookRequest) (*types.QueryAllLongBookResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)

	longBooks, pageRes, err := k.GetAllLongBookForPairPaginated(
		ctx,
		req.ContractAddr,
		req.PriceDenom,
		req.AssetDenom,
		req.Pagination,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllLongBookResponse{LongBook: longBooks, Pagination: pageRes}, nil
}

func (k KeeperWrapper) LongBook(c context.Context, req *types.QueryGetLongBookRequest) (*types.QueryGetLongBookResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)
	price, err := sdk.NewDecFromStr(req.Price)
	if err != nil {
		return nil, err
	}
	longBook, found := k.GetLongBookByPrice(ctx, req.ContractAddr, price, req.PriceDenom, req.AssetDenom)
	if !found {
		return nil, sdkerrors.ErrKeyNotFound
	}

	return &types.QueryGetLongBookResponse{LongBook: longBook}, nil
}

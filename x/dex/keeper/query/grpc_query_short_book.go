package query

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k KeeperWrapper) ShortBookAll(c context.Context, req *types.QueryAllShortBookRequest) (*types.QueryAllShortBookResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)

	shortBooks, pageRes, err := k.GetAllShortBookForPairPaginated(
		ctx, req.ContractAddr, req.PriceDenom, req.AssetDenom, req.Pagination,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllShortBookResponse{ShortBook: shortBooks, Pagination: pageRes}, nil
}

func (k KeeperWrapper) ShortBook(c context.Context, req *types.QueryGetShortBookRequest) (*types.QueryGetShortBookResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)
	price, err := sdk.NewDecFromStr(req.Price)
	if err != nil {
		return nil, err
	}
	shortBook, found := k.GetShortBookByPrice(ctx, req.ContractAddr, price, req.PriceDenom, req.AssetDenom)
	if !found {
		return nil, sdkerrors.ErrKeyNotFound
	}

	return &types.QueryGetShortBookResponse{ShortBook: shortBook}, nil
}

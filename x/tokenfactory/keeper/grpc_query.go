package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

var _ types.QueryServer = Keeper{}

func (k Keeper) Params(ctx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params := k.GetParams(sdkCtx)

	return &types.QueryParamsResponse{Params: params}, nil
}

func (k Keeper) DenomAuthorityMetadata(ctx context.Context, req *types.QueryDenomAuthorityMetadataRequest) (*types.QueryDenomAuthorityMetadataResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	authorityMetadata, err := k.GetAuthorityMetadata(sdkCtx, req.GetDenom())
	if err != nil {
		return nil, err
	}

	return &types.QueryDenomAuthorityMetadataResponse{AuthorityMetadata: authorityMetadata}, nil
}

func (k Keeper) DenomsFromCreator(ctx context.Context, req *types.QueryDenomsFromCreatorRequest) (*types.QueryDenomsFromCreatorResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	denoms := k.getDenomsFromCreator(sdkCtx, req.GetCreator())
	return &types.QueryDenomsFromCreatorResponse{Denoms: denoms}, nil
}

// DenomMetadata implements Query/DenomMetadata gRPC method.
func (k Keeper) DenomMetadata(c context.Context, req *types.QueryDenomMetadataRequest) (*types.QueryDenomMetadataResponse, error) {
	if req == nil {
		return nil, status.Errorf(codes.InvalidArgument, "empty request")
	}

	if req.Denom == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid denom")
	}

	ctx := sdk.UnwrapSDKContext(c)

	metadata, found := k.bankKeeper.GetDenomMetaData(ctx, req.Denom)
	if !found {
		return nil, status.Errorf(codes.NotFound, "client metadata for denom %s", req.Denom)
	}

	return &types.QueryDenomMetadataResponse{
		Metadata: metadata,
	}, nil
}

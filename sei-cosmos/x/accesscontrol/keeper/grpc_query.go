package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

var _ types.QueryServer = Keeper{}

func (k Keeper) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params := k.GetParams(sdkCtx)

	return &types.QueryParamsResponse{Params: params}, nil
}

func (k Keeper) ResourceDepedencyMappingFromMessageKey(ctx context.Context, req *types.ResourceDepedencyMappingFromMessageKeyRequest) (*types.ResourceDepedencyMappingFromMessageKeyResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	resourceDependency := k.GetResourceDependencyMapping(sdkCtx, req.GetMessageKey())
	return &types.ResourceDepedencyMappingFromMessageKeyResponse{MessageDependencyMapping: resourceDependency}, nil
}


func (k Keeper) ListResourceDepedencyMapping(ctx context.Context, req *types.ListResourceDepedencyMappingRequest) (*types.ListResourceDepedencyMappingResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	resourceDepedencyMappings := []acltypes.MessageDependencyMapping{}
	k.IterateResourceKeys(sdkCtx, func(dependencyMapping acltypes.MessageDependencyMapping) (stop bool) {
		resourceDepedencyMappings = append(resourceDepedencyMappings, dependencyMapping)
		return false
	})

	return &types.ListResourceDepedencyMappingResponse{MessageDependencyMappingList: resourceDepedencyMappings}, nil
}

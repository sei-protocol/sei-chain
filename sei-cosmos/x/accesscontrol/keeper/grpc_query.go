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

func (k Keeper) ResourceDependencyMappingFromMessageKey(ctx context.Context, req *types.ResourceDependencyMappingFromMessageKeyRequest) (*types.ResourceDependencyMappingFromMessageKeyResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	resourceDependency := k.GetResourceDependencyMapping(sdkCtx, types.MessageKey(req.GetMessageKey()))
	return &types.ResourceDependencyMappingFromMessageKeyResponse{MessageDependencyMapping: resourceDependency}, nil
}

func (k Keeper) WasmFunctionDependencyMapping(ctx context.Context, req *types.WasmFunctionDependencyMappingRequest) (*types.WasmFunctionDependencyMappingResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	wasmDependency, err := k.GetWasmFunctionDependencyMapping(sdkCtx, req.CodeId, req.WasmFunction)
	if err != nil {
		return nil, err
	}
	return &types.WasmFunctionDependencyMappingResponse{WasmFunctionDependencyMapping: wasmDependency}, nil
}

func (k Keeper) ListResourceDependencyMapping(ctx context.Context, req *types.ListResourceDependencyMappingRequest) (*types.ListResourceDependencyMappingResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	resourceDependencyMappings := []acltypes.MessageDependencyMapping{}
	k.IterateResourceKeys(sdkCtx, func(dependencyMapping acltypes.MessageDependencyMapping) (stop bool) {
		resourceDependencyMappings = append(resourceDependencyMappings, dependencyMapping)
		return false
	})

	return &types.ListResourceDependencyMappingResponse{MessageDependencyMappingList: resourceDependencyMappings}, nil
}

func (k Keeper) ListWasmFunctionDependencyMapping(ctx context.Context, req *types.ListWasmFunctionDependencyMappingRequest) (*types.ListWasmFunctionDependencyMappingResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	wasmDependencyMappings := []acltypes.WasmFunctionDependencyMapping{}
	k.IterateWasmDependencies(sdkCtx, func(dependencyMapping acltypes.WasmFunctionDependencyMapping) (stop bool) {
		wasmDependencyMappings = append(wasmDependencyMappings, dependencyMapping)
		return false
	})

	return &types.ListWasmFunctionDependencyMappingResponse{WasmFunctionDependencyMappingList: wasmDependencyMappings}, nil
}

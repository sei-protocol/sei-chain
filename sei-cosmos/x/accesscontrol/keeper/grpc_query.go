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

func (k Keeper) WasmDependencyMapping(ctx context.Context, req *types.WasmDependencyMappingRequest) (*types.WasmDependencyMappingResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	address, err := sdk.AccAddressFromBech32(req.ContractAddress)
	if err != nil {
		return nil, err
	}
	wasmDependency, err := k.GetRawWasmDependencyMapping(sdkCtx, address)
	if err != nil {
		return nil, err
	}
	return &types.WasmDependencyMappingResponse{WasmDependencyMapping: *wasmDependency}, nil
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

func (k Keeper) ListWasmDependencyMapping(ctx context.Context, req *types.ListWasmDependencyMappingRequest) (*types.ListWasmDependencyMappingResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	wasmDependencyMappings := []acltypes.WasmDependencyMapping{}
	k.IterateWasmDependencies(sdkCtx, func(dependencyMapping acltypes.WasmDependencyMapping) (stop bool) {
		wasmDependencyMappings = append(wasmDependencyMappings, dependencyMapping)
		return false
	})

	return &types.ListWasmDependencyMappingResponse{WasmDependencyMappingList: wasmDependencyMappings}, nil
}

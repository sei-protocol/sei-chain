package wasm

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	tokenfactorykeeper "github.com/sei-protocol/sei-chain/x/tokenfactory/keeper"
	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

type TokenFactoryWasmQueryHandler struct {
	tokenfactoryKeeper tokenfactorykeeper.Keeper
}

func NewTokenFactoryWasmQueryHandler(keeper *tokenfactorykeeper.Keeper) *TokenFactoryWasmQueryHandler {
	return &TokenFactoryWasmQueryHandler{
		tokenfactoryKeeper: *keeper,
	}
}

func (handler TokenFactoryWasmQueryHandler) GetDenomAuthorityMetadata(ctx sdk.Context, req *types.QueryDenomAuthorityMetadataRequest) (*types.QueryDenomAuthorityMetadataResponse, error) {
	c := sdk.WrapSDKContext(ctx)
	return handler.tokenfactoryKeeper.DenomAuthorityMetadata(c, req)
}

func (handler TokenFactoryWasmQueryHandler) GetDenomsFromCreator(ctx sdk.Context, req *types.QueryDenomsFromCreatorRequest) (*types.QueryDenomsFromCreatorResponse, error) {
	c := sdk.WrapSDKContext(ctx)
	return handler.tokenfactoryKeeper.DenomsFromCreator(c, req)
}

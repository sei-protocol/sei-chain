package wasm

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	tokenfactorykeeper "github.com/sei-protocol/sei-chain/x/tokenfactory/keeper"
	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

// defaultDenomsFromCreatorLimit is the wasm query default, higher than the gRPC DefaultLimit of 100
// for backwards compatibility with contracts that expect all denoms in one response, but still
// bounded to limit DoS risk since denom creation has no fee.
const defaultDenomsFromCreatorLimit = 2000

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
	if req.Pagination == nil {
		req.Pagination = &query.PageRequest{Limit: defaultDenomsFromCreatorLimit}
	}
	return handler.tokenfactoryKeeper.DenomsFromCreator(c, req)
}

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

func (handler TokenFactoryWasmQueryHandler) GetDenomCreationFeeWhitelist(ctx sdk.Context) (*types.QueryDenomCreationFeeWhitelistResponse, error) {
	c := sdk.WrapSDKContext(ctx)
	return handler.tokenfactoryKeeper.DenomCreationFeeWhitelist(c, &types.QueryDenomCreationFeeWhitelistRequest{})
}

func (handler TokenFactoryWasmQueryHandler) GetCreatorInDenomFeeWhitelist(ctx sdk.Context, req *types.QueryCreatorInDenomFeeWhitelistRequest) (*types.QueryCreatorInDenomFeeWhitelistResponse, error) {
	c := sdk.WrapSDKContext(ctx)
	return handler.tokenfactoryKeeper.CreatorInDenomFeeWhitelist(c, req)
}

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

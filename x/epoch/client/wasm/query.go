package wasm

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/epoch/keeper"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
)

type EpochWasmQueryHandler struct {
	epochKeeper keeper.Keeper
}

func NewEpochWasmQueryHandler(keeper *keeper.Keeper) *EpochWasmQueryHandler {
	return &EpochWasmQueryHandler{
		epochKeeper: *keeper,
	}
}

func (handler EpochWasmQueryHandler) GetEpoch(ctx sdk.Context, req *types.QueryEpochRequest) (*types.QueryEpochResponse, error) {
	c := sdk.WrapSDKContext(ctx)
	return handler.epochKeeper.Epoch(c, req)
}

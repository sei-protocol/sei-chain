package keeper

import (
	"context"

	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) Liquidate(goCtx context.Context, msg *types.MsgLiquidation) (*types.MsgLiquidationResponse, error) {
	_, span := (*k.tracingInfo.Tracer).Start(goCtx, "PlaceOrders")
	defer span.End()

	liquidationRequestorToAccount := k.LiquidationRequests[msg.GetContractAddr()]
	if _, ok := liquidationRequestorToAccount[msg.GetAccountToLiquidate()]; !ok {
		liquidationRequestorToAccount[msg.GetAccountToLiquidate()] = msg.GetAccountToLiquidate()
	}

	return &types.MsgLiquidationResponse{}, nil
}

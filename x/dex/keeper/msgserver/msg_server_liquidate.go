package msgserver

import (
	"context"

	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
)

func (k msgServer) Liquidate(goCtx context.Context, msg *types.MsgLiquidation) (*types.MsgLiquidationResponse, error) {
	k.MemState.GetLiquidationRequests(
		typesutils.ContractAddress(msg.GetContractAddr()),
	).Add(&dexcache.LiquidationRequest{Requestor: msg.Creator, AccountToLiquidate: msg.AccountToLiquidate})

	return &types.MsgLiquidationResponse{}, nil
}

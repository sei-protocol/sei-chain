package msgserver

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
)

func (k msgServer) Liquidate(goCtx context.Context, msg *types.MsgLiquidation) (*types.MsgLiquidationResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	k.MemState.GetLiquidationRequests(
		ctx, typesutils.ContractAddress(msg.GetContractAddr()),
	).Add(&dexcache.LiquidationRequest{Requestor: msg.Creator, AccountToLiquidate: msg.AccountToLiquidate})

	return &types.MsgLiquidationResponse{}, nil
}

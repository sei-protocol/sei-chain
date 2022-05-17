package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) RegisterContract(goCtx context.Context, msg *types.MsgRegisterContract) (*types.MsgRegisterContractResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	for _, contractAddr := range k.GetAllContractAddresses(ctx) {
		if msg.Contract.ContractAddr == contractAddr {
			return &types.MsgRegisterContractResponse{}, nil
		}
	}
	contractAddr := msg.Contract.ContractAddr
	k.SetContractAddress(ctx, contractAddr, msg.Contract.CodeId)
	k.Orders[contractAddr] = map[string]*dexcache.Orders{}
	k.OrderPlacements[contractAddr] = map[string]*dexcache.OrderPlacements{}
	k.OrderCancellations[contractAddr] = map[string]*dexcache.OrderCancellations{}
	k.DepositInfo[contractAddr] = dexcache.NewDepositInfo()
	k.LiquidationRequests[contractAddr] = map[string]string{}

	return &types.MsgRegisterContractResponse{}, nil
}

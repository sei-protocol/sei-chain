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
	typedContractAddr := types.ContractAddress(contractAddr)
	k.BlockOrders[typedContractAddr] = map[types.PairString]*dexcache.BlockOrders{}
	k.BlockCancels[typedContractAddr] = map[types.PairString]*dexcache.BlockCancellations{}
	k.DepositInfo[typedContractAddr] = dexcache.NewDepositInfo()
	k.LiquidationRequests[typedContractAddr] = &dexcache.LiquidationRequests{}

	return &types.MsgRegisterContractResponse{}, nil
}

package msgserver

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) RegisterPairs(goCtx context.Context, msg *types.MsgRegisterPairs) (*types.MsgRegisterPairsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Loop through each batch contract pair an individual contract pair, token pair
	// tuple and register them individually
	for _, batchContractPair := range msg.Batchcontractpair {
		contractAddress := batchContractPair.ContractAddr
		for _, pair := range batchContractPair.Pairs {
			k.AddRegisteredPair(ctx, contractAddress, *pair)
			k.SetTickSizeForPair(ctx, contractAddress, *pair, *pair.Ticksize)
		}
	}

	return &types.MsgRegisterPairsResponse{}, nil
}

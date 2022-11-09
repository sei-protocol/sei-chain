package msgserver

import (
	"context"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) RegisterPairs(goCtx context.Context, msg *types.MsgRegisterPairs) (*types.MsgRegisterPairsResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	// Validation such that only the user who stored the code can register pairs
	for _, batchPair := range msg.Batchcontractpair {
		contractAddr := batchPair.ContractAddr
		contractInfo, err := k.GetContract(ctx, contractAddr)
		if err != nil {
			return nil, err
		}

		if msg.Creator != contractInfo.Creator {
			return nil, errors.New("Only contract creator can update registered pairs")
		}
	}

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

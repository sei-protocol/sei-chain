package msgserver

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) RegisterPairs(goCtx context.Context, msg *types.MsgRegisterPairs) (*types.MsgRegisterPairsResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	events := []sdk.Event{}
	// Validation such that only the user who stored the code can register pairs
	for _, batchPair := range msg.Batchcontractpair {
		contractAddr := batchPair.ContractAddr
		contractInfo, err := k.GetContract(ctx, contractAddr)
		if err != nil {
			return nil, err
		}

		if msg.Creator != contractInfo.Creator {
			return nil, sdkerrors.ErrUnauthorized
		}

		// Loop through each batch contract pair an individual contract pair, token pair
		// tuple and register them individually
		for _, pair := range batchPair.Pairs {
			k.AddRegisteredPair(ctx, contractAddr, *pair)
			events = append(events, sdk.NewEvent(
				types.EventTypeRegisterPair,
				sdk.NewAttribute(types.AttributeKeyContractAddress, contractAddr),
				sdk.NewAttribute(types.AttributeKeyPriceDenom, pair.PriceDenom),
				sdk.NewAttribute(types.AttributeKeyAssetDenom, pair.AssetDenom),
			))
		}
	}

	ctx.EventManager().EmitEvents(events)

	return &types.MsgRegisterPairsResponse{}, nil
}

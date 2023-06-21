package msgserver

import (
	"context"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) RegisterPairs(goCtx context.Context, msg *types.MsgRegisterPairs) (*types.MsgRegisterPairsResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	maxPairsPerContract := k.GetMaxPairsPerContract(ctx)
	events := []sdk.Event{}
	// Validation such that only the user who stored the code can register pairs
	for _, batchPair := range msg.Batchcontractpair {
		contractAddr := batchPair.ContractAddr
		existingPairCount := uint64(len(k.GetAllRegisteredPairs(ctx, contractAddr)))
		if existingPairCount >= maxPairsPerContract {
			return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "contract %s already has %d pairs registered", contractAddr, existingPairCount)
		}
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
			if !isValidDenom(pair.PriceDenom) || !isValidDenom(pair.AssetDenom) {
				return nil, sdkerrors.ErrInvalidRequest
			}
			added := k.AddRegisteredPair(ctx, contractAddr, *pair)

			if !added {
				// If its already added then no event is emitted
				continue
			}
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

func isValidDenom(denom string) bool {
	return denom != "" && !strings.Contains(denom, types.PairDelim)
}

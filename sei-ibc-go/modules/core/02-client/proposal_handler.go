package client

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/keeper"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
)

// NewClientProposalHandler defines the 02-client proposal handler
func NewClientProposalHandler(_ keeper.Keeper) govtypes.Handler {
	return func(_ sdk.Context, content govtypes.Content) error {
		switch c := content.(type) {
		case *types.ClientUpdateProposal, *types.UpgradeProposal:
			return types.ErrClientDeprecated
		default:
			return sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "unrecognized ibc proposal content type: %T", c)
		}
	}
}

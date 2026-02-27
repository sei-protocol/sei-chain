package tokenfactory

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"

	"github.com/sei-protocol/sei-chain/x/tokenfactory/keeper"
)

func NewProposalHandler(_ keeper.Keeper) govtypes.Handler {
	return func(ctx sdk.Context, content govtypes.Content) error {
		return sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "unrecognized tokenfactory proposal content type")
	}
}

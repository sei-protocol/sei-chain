package tokenfactory

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/sei-protocol/sei-chain/x/tokenfactory/keeper"
)

func NewProposalHandler(_ keeper.Keeper) govtypes.Handler {
	return func(ctx sdk.Context, content govtypes.Content) error {
		return sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "unrecognized tokenfactory proposal content type")
	}
}

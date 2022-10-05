package accesscontrol

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

func HandleMsgUpdateResourceDependencyMappingProposal(ctx sdk.Context, k *keeper.Keeper, p *types.MsgUpdateResourceDependencyMappingProposal) error {
	for _, resourceDepMapping := range p.MessageDependencyMapping {
		k.SetResourceDependencyMapping(ctx, resourceDepMapping)
	}
	return nil
}

func NewProposalHandler(k keeper.Keeper) govtypes.Handler {
	return func(ctx sdk.Context, content govtypes.Content) error {
		switch c := content.(type) {
		case *types.MsgUpdateResourceDependencyMappingProposal:
			return HandleMsgUpdateResourceDependencyMappingProposal(ctx, &k, c)
		default:
			return sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "unrecognized accesscontrol proposal content type: %T", c)
		}
	}
}

func (am AppModule) GetMessageDependencies(ctx sdk.Context, msg sdk.Msg) []acltypes.AccessOperation {
	switch msg.(type) {
	default:
		// Default behavior is to get the static dependency mapping for the message
		messageKey := types.GenerateMessageKey(msg)
		dependencyMapping := am.keeper.GetResourceDependencyMapping(ctx, messageKey)
		return dependencyMapping.AccessOps
	}
}

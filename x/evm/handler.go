package evm

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func NewHandler(k *keeper.Keeper) sdk.Handler {
	msgServer := keeper.NewMsgServerImpl(k)

	return func(ctx sdk.Context, msg sdk.Msg) (*sdk.Result, error) {
		ctx = ctx.WithEventManager(sdk.NewEventManager())

		switch msg := msg.(type) {
		case *types.MsgEVMTransaction:
			res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), msg)
			return sdk.WrapServiceResult(ctx, res, err)
		case *types.MsgSend:
			res, err := msgServer.Send(sdk.WrapSDKContext(ctx), msg)
			return sdk.WrapServiceResult(ctx, res, err)
		case *types.MsgRegisterPointer:
			res, err := msgServer.RegisterPointer(sdk.WrapSDKContext(ctx), msg)
			return sdk.WrapServiceResult(ctx, res, err)
		case *types.MsgAssociateContractAddress:
			res, err := msgServer.AssociateContractAddress(sdk.WrapSDKContext(ctx), msg)
			return sdk.WrapServiceResult(ctx, res, err)
		default:
			errMsg := fmt.Sprintf("unrecognized %s message type: %T", types.ModuleName, msg)
			return nil, sdkerrors.Wrap(sdkerrors.ErrUnknownRequest, errMsg)
		}
	}
}

func NewProposalHandler(k keeper.Keeper) govtypes.Handler {
	return func(ctx sdk.Context, content govtypes.Content) error {
		switch c := content.(type) {
		case *types.AddERCNativePointerProposal:
			return HandleAddERCNativePointerProposal(ctx, &k, c)
		case *types.AddERCCW20PointerProposal:
			return HandleAddERCCW20PointerProposal(ctx, &k, c)
		case *types.AddERCCW721PointerProposal:
			return HandleAddERCCW721PointerProposal(ctx, &k, c)
		case *types.AddCWERC20PointerProposal:
			return HandleAddCWERC20PointerProposal(ctx, &k, c)
		case *types.AddCWERC721PointerProposal:
			return HandleAddCWERC721PointerProposal(ctx, &k, c)
		case *types.AddERCNativePointerProposalV2:
			return HandleAddERCNativePointerProposalV2(ctx, &k, c)
		default:
			return sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "unrecognized evm proposal content type: %T", c)
		}
	}
}

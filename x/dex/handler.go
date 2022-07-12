package dex

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/sei-protocol/sei-chain/utils/tracing"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// NewHandler ...
func NewHandler(k keeper.Keeper, tracingInfo *tracing.TracingInfo) sdk.Handler {
	msgServer := keeper.NewMsgServerImpl(k, tracingInfo)

	return func(ctx sdk.Context, msg sdk.Msg) (*sdk.Result, error) {
		ctx = ctx.WithEventManager(sdk.NewEventManager())

		switch msg := msg.(type) {
		case *types.MsgPlaceOrders:
			res, err := msgServer.PlaceOrders(sdk.WrapSDKContext(ctx), msg)
			return sdk.WrapServiceResult(ctx, res, err)
		case *types.MsgCancelOrders:
			res, err := msgServer.CancelOrders(sdk.WrapSDKContext(ctx), msg)
			return sdk.WrapServiceResult(ctx, res, err)
		case *types.MsgLiquidation:
			res, err := msgServer.Liquidate(sdk.WrapSDKContext(ctx), msg)
			return sdk.WrapServiceResult(ctx, res, err)
		case *types.MsgRegisterContract:
			res, err := msgServer.RegisterContract(sdk.WrapSDKContext(ctx), msg)
			return sdk.WrapServiceResult(ctx, res, err)
			// this line is used by starport scaffolding # 1
		default:
			errMsg := fmt.Sprintf("unrecognized %s message type: %T", types.ModuleName, msg)
			return nil, sdkerrors.Wrap(sdkerrors.ErrUnknownRequest, errMsg)
		}
	}
}

func NewProposalHandler(k keeper.Keeper) govtypes.Handler {
	return func(ctx sdk.Context, content govtypes.Content) error {
		switch c := content.(type) {
		case *types.RegisterPairsProposal:
			return k.HandleRegisterPairsProposal(ctx, c)
		case *types.UpdateTickSizeProposal:
			return k.HandleUpdateTickSizeProposal(ctx, c)
		case *types.AddAssetMetadataProposal:
			return k.HandleAddAssetMetadataProposal(ctx, c)
		default:
			return sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "unrecognized dex proposal content type: %T", c)
		}
	}
}

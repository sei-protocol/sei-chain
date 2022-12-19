package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the acl MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

func (k msgServer) RegisterWasmDependency(goCtx context.Context, msg *types.MsgRegisterWasmDependency) (*types.MsgRegisterWasmDependencyResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	err := k.SetWasmDependencyMapping(ctx, msg.WasmDependencyMapping)
	if err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.ModuleName),
		),
	)

	return &types.MsgRegisterWasmDependencyResponse{}, nil
}

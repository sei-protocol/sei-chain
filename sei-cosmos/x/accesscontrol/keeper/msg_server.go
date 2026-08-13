package keeper

import (
	"context"

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
	return &types.MsgRegisterWasmDependencyResponse{}, nil
}

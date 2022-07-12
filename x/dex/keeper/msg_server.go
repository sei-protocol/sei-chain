package keeper

import (
	"github.com/sei-protocol/sei-chain/utils/tracing"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type msgServer struct {
	Keeper
	tracingInfo *tracing.Info
}

// NewMsgServerImpl returns an implementation of the MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper, tracingInfo *tracing.Info) types.MsgServer {
	return &msgServer{Keeper: keeper, tracingInfo: tracingInfo}
}

var _ types.MsgServer = msgServer{}

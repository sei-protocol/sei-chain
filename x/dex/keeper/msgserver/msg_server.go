package msgserver

import (
	"github.com/sei-protocol/sei-chain/utils/tracing"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type msgServer struct {
	keeper.Keeper
	tracingInfo *tracing.Info
}

// NewMsgServerImpl returns an implementation of the MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper keeper.Keeper, tracingInfo *tracing.Info) types.MsgServer {
	return &msgServer{Keeper: keeper, tracingInfo: tracingInfo}
}

var _ types.MsgServer = msgServer{}

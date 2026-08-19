package keeper

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/types"
)

// DeprecatedMsgServer is a types.MsgServer that rejects every message with
// types.ErrTransferDeprecated.
//
// It is registered as the transfer module's message server so that submitted
// transactions are rejected, while Keeper retains the executable transfer
// logic for the versioned EVM precompiles that replay historical blocks.
type DeprecatedMsgServer struct{}

var _ types.MsgServer = DeprecatedMsgServer{}

// Transfer defines an RPC handler for MsgTransfer.
func (DeprecatedMsgServer) Transfer(context.Context, *types.MsgTransfer) (*types.MsgTransferResponse, error) {
	return nil, types.ErrTransferDeprecated
}

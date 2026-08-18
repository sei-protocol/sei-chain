package keeper

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/types"
)

var _ types.MsgServer = Keeper{}

// Transfer defines an RPC handler for MsgTransfer.
func (Keeper) Transfer(context.Context, *types.MsgTransfer) (*types.MsgTransferResponse, error) {
	return nil, types.ErrTransferDeprecated
}

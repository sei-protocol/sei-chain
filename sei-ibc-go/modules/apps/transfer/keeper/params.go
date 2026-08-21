package keeper

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/types"
)

// GetSendEnabled retrieves the send enabled boolean from the paramstore
func (k Keeper) GetSendEnabled(ctx sdk.Context) bool {
	var res bool
	k.paramSpace.Get(ctx, types.KeySendEnabled, &res)
	return res
}

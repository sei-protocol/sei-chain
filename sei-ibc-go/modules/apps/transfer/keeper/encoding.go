package keeper

import (
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/types"
)

// MustUnmarshalDenomTrace attempts to decode and return an DenomTrace object from
// raw encoded bytes. It panics on error.
func (k Keeper) MustUnmarshalDenomTrace(bz []byte) types.DenomTrace {
	var denomTrace types.DenomTrace
	k.cdc.MustUnmarshal(bz, &denomTrace)
	return denomTrace
}

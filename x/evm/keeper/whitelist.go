package keeper

import (
	"bytes"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k *Keeper) IsCWCodeHashWhitelistedForEVMDelegateCall(ctx sdk.Context, h []byte) bool {
	for _, w := range k.WhitelistedCwCodeHashesForDelegateCall(ctx) {
		if bytes.Equal(w, h) {
			return true
		}
	}
	return false
}

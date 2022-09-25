package keeper

import (
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k Keeper) SetSender(ctx sdk.Context, slot uint64, sender string) {
	store := k.GetSenderStore(ctx)
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, slot)
	store.Set(key, []byte(sender))
}

func (k Keeper) GetSender(ctx sdk.Context, slot uint64) (string, bool) {
	store := k.GetSenderStore(ctx)
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, slot)
	bz := store.Get(key)
	if bz == nil {
		return "", false
	}
	return string(bz), true
}

package keeper

import (
	"encoding/binary"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const EpochKey = "epoch"

func (k Keeper) SetEpoch(ctx sdk.Context, epoch uint64) {
	store := ctx.KVStore(k.storeKey)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, epoch)
	store.Set([]byte(EpochKey), bz)
	ctx.Logger().Info(fmt.Sprintf("Current epoch %d", epoch))
}

func (k Keeper) IsNewEpoch(ctx sdk.Context) (bool, uint64) {
	store := ctx.KVStore(k.storeKey)
	b := store.Get([]byte(EpochKey))
	lastEpoch := binary.BigEndian.Uint64(b)
	currentEpoch := k.EpochKeeper.GetEpoch(ctx).CurrentEpoch
	return currentEpoch > lastEpoch, currentEpoch
}

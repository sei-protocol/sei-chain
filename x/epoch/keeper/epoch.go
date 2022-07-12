package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gogo/protobuf/proto"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
)

const EpochKey = "epoch"

func (k Keeper) SetEpoch(ctx sdk.Context, epoch types.Epoch) {
	store := ctx.KVStore(k.storeKey)
	value, err := proto.Marshal(&epoch)
	if err != nil {
		panic(err)
	}
	store.Set([]byte(EpochKey), value)
}

func (k Keeper) GetEpoch(ctx sdk.Context) (epoch types.Epoch) {
	store := ctx.KVStore(k.storeKey)
	b := store.Get([]byte(EpochKey))
	k.cdc.MustUnmarshal(b, &epoch)
	return epoch
}

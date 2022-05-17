package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gogo/protobuf/proto"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
)

const EPOCH_KEY = "epoch"

func (k Keeper) SetEpoch(ctx sdk.Context, epoch types.Epoch) {
	store := ctx.KVStore(k.storeKey)
	value, err := proto.Marshal(&epoch)
	if err != nil {
		panic(err)
	}
	store.Set([]byte(EPOCH_KEY), value)
}

func (k Keeper) GetEpoch(ctx sdk.Context) (epoch types.Epoch) {
	store := ctx.KVStore(k.storeKey)
	b := store.Get([]byte(EPOCH_KEY))
	k.cdc.MustUnmarshal(b, &epoch)
	return epoch
}

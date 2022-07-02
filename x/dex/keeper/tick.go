package keeper

import (
	"encoding/binary"
	"math"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// pair -> tick size
// todo use proposal to change the tick size?
func (k Keeper) SetTickSizeForPair(ctx sdk.Context, pair types.Pair, ticksize float32) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefix("ticks"))
	store.Set(types.PairPrefix(pair.PriceDenom, pair.AssetDenom), float32ToByte(ticksize))
}

func (k Keeper) GetTickSizeForPair(ctx sdk.Context, pair types.Pair) (float32, bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefix("ticks"))
	b := store.Get(types.PairPrefix(pair.PriceDenom, pair.AssetDenom))
	if b == nil {
		return -1, false
	}
	return float32frombytes(b), true
}

// use bigindian encoding for now
func float32ToByte(f float32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], math.Float32bits(f))
	return buf[:]
}

func float32frombytes(bytes []byte) float32 {
    bits := binary.BigEndian.Uint32(bytes)
    return math.Float32frombits(bits)
}

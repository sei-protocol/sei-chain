package keeper

import (
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) SetMatchResult(ctx sdk.Context, contractAddr string, result *types.MatchResult) {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		types.MatchResultPrefix(contractAddr),
	)
	height := ctx.BlockHeight()
	result.Height = height
	result.ContractAddr = contractAddr
	bz, err := result.Marshal()
	if err != nil {
		panic(err)
	}
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, uint64(height))
	store.Set(key, bz)
}

func (k Keeper) GetMatchResult(ctx sdk.Context, contractAddr string, height int64) (*types.MatchResult, bool) {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		types.MatchResultPrefix(contractAddr),
	)
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, uint64(height))
	if !store.Has(key) {
		return nil, false
	}
	bz := store.Get(key)
	result := types.MatchResult{}
	if err := result.Unmarshal(bz); err != nil {
		panic(err)
	}
	return &result, true
}

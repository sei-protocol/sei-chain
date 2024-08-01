package keeper

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const MatchResultKey = "match-result"

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
	store.Set([]byte(MatchResultKey), bz)
}

func (k Keeper) GetMatchResultState(ctx sdk.Context, contractAddr string) (*types.MatchResult, bool) {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		types.MatchResultPrefix(contractAddr),
	)
	bz := store.Get([]byte(MatchResultKey))
	result := types.MatchResult{}
	if err := result.Unmarshal(bz); err != nil {
		panic(err)
	}
	return &result, true
}

func (k Keeper) DeleteMatchResultState(ctx sdk.Context, contractAddr string) {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		types.MatchResultPrefix(contractAddr),
	)
	store.Delete([]byte(MatchResultKey))
}

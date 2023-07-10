package migrations

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/goutils"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const OldPairSeparator = '|'

func OldPairPrefix(priceDenom string, assetDenom string) []byte {
	return goutils.ImmutableAppend(goutils.ImmutableAppend([]byte(priceDenom), OldPairSeparator), []byte(assetDenom)...)
}

func V16ToV17(ctx sdk.Context, dexkeeper keeper.Keeper) error {
	rootStore := ctx.KVStore(dexkeeper.GetStoreKey())

	handler := func(oldPref []byte, newPref []byte) {
		store := prefix.NewStore(rootStore, oldPref)
		kv := map[string][]byte{}
		iter := store.Iterator(nil, nil)
		defer iter.Close()
		for ; iter.Valid(); iter.Next() {
			kv[string(iter.Key())] = iter.Value()
		}
		for k, v := range kv {
			kbz := []byte(k)
			store.Delete(kbz)
			rootStore.Set(goutils.ImmutableAppend(newPref, kbz...), v)
		}
	}

	for _, c := range dexkeeper.GetAllContractInfo(ctx) {
		for _, p := range dexkeeper.GetAllRegisteredPairs(ctx, c.ContractAddr) {
			// long order book
			handler(
				goutils.ImmutableAppend(
					types.OrderBookContractPrefix(true, c.ContractAddr),
					OldPairPrefix(p.PriceDenom, p.AssetDenom)...,
				),
				types.OrderBookPrefix(true, c.ContractAddr, p.PriceDenom, p.AssetDenom),
			)

			// short order book
			handler(
				goutils.ImmutableAppend(
					types.OrderBookContractPrefix(false, c.ContractAddr),
					OldPairPrefix(p.PriceDenom, p.AssetDenom)...,
				),
				types.OrderBookPrefix(false, c.ContractAddr, p.PriceDenom, p.AssetDenom),
			)

			// price
			handler(
				goutils.ImmutableAppend(
					types.PriceContractPrefix(c.ContractAddr),
					OldPairPrefix(p.PriceDenom, p.AssetDenom)...,
				),
				types.PricePrefix(c.ContractAddr, p.PriceDenom, p.AssetDenom),
			)

			// order count (long)
			handler(
				goutils.ImmutableAppend(
					goutils.ImmutableAppend(types.KeyPrefix(types.LongOrderCountKey), types.AddressKeyPrefix(c.ContractAddr)...),
					OldPairPrefix(p.PriceDenom, p.AssetDenom)...,
				),
				types.OrderCountPrefix(c.ContractAddr, p.PriceDenom, p.AssetDenom, true),
			)

			// order count (short)
			handler(
				goutils.ImmutableAppend(
					goutils.ImmutableAppend(types.KeyPrefix(types.ShortOrderCountKey), types.AddressKeyPrefix(c.ContractAddr)...),
					OldPairPrefix(p.PriceDenom, p.AssetDenom)...,
				),
				types.OrderCountPrefix(c.ContractAddr, p.PriceDenom, p.AssetDenom, false),
			)

			// registered pair
			k := goutils.ImmutableAppend(types.RegisteredPairPrefix(c.ContractAddr), OldPairPrefix(p.PriceDenom, p.AssetDenom)...)
			pair := rootStore.Get(k)
			rootStore.Delete(k)
			rootStore.Set(
				goutils.ImmutableAppend(types.RegisteredPairPrefix(c.ContractAddr), types.PairPrefix(p.PriceDenom, p.AssetDenom)...),
				pair,
			)
		}
	}

	// asset list
	pref := types.KeyPrefix(types.AssetListKey)
	store := prefix.NewStore(rootStore, pref)
	kv := map[string][]byte{}
	iter := store.Iterator(nil, nil)
	for ; iter.Valid(); iter.Next() {
		kv[string(iter.Key())] = iter.Value()
	}
	iter.Close()
	for k, v := range kv {
		kbz := []byte(k)
		store.Delete(kbz)
		denom := string(kbz)
		newKbz := types.AssetListPrefix(denom)
		rootStore.Set(newKbz, v)
	}
	return nil
}

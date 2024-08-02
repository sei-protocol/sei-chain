package migrations

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const PriceTickSizeKey = "ticks"
const QuantityTickSizeKey = "quantityticks"
const RegisteredPairCount = "rpcnt"

// This migration deprecates the tick size store keys and registered pair count store keys.
// It also refactors the registered pair store to use pair denoms for key construction
// instead of index based key construction.
func V9ToV10(ctx sdk.Context, dexkeeper keeper.Keeper) error {
	dexkeeper.CreateModuleAccount(ctx)
	store := ctx.KVStore(dexkeeper.GetStoreKey())

	rpIterator := sdk.KVStorePrefixIterator(store, []byte(types.RegisteredPairKey))

	rpcntByte := []byte(RegisteredPairCount)
	defer rpIterator.Close()
	for ; rpIterator.Valid(); rpIterator.Next() {
		// need to skip anything that has rpcnt in the key prefix
		if string(rpIterator.Key()[:len(rpcntByte)]) == RegisteredPairCount {
			continue
		}
		var registeredPair types.Pair
		b := rpIterator.Value()
		err := registeredPair.Unmarshal(b)
		if err != nil {
			return err
		}
		// this key is the contractAddress + index
		rpKey := rpIterator.Key()
		// remove first 2 bytes for prefix and last 8 bytes since that is the index
		rpContractKey := rpKey[2 : len(rpKey)-8]
		// get pair prefix used for indexing
		pairPrefix := types.PairPrefix(registeredPair.PriceDenom, registeredPair.AssetDenom)

		// set the price and quantity ticks from the appropriate store
		priceTickStore := prefix.NewStore(store, append([]byte(PriceTickSizeKey), rpContractKey...))
		priceTickSize := sdk.Dec{}
		b = priceTickStore.Get(pairPrefix)
		err = priceTickSize.Unmarshal(b)
		if err != nil {
			return err
		}
		registeredPair.PriceTicksize = &priceTickSize

		quantityTickStore := prefix.NewStore(store, append([]byte(QuantityTickSizeKey), rpContractKey...))
		quantityTickSize := sdk.Dec{}
		b = quantityTickStore.Get(pairPrefix)
		err = quantityTickSize.Unmarshal(b)
		if err != nil {
			return err
		}
		registeredPair.QuantityTicksize = &quantityTickSize

		// delete the old store value
		rpStore := prefix.NewStore(store, append([]byte(types.RegisteredPairKey), rpContractKey...))
		rpStore.Delete(rpKey)
		// updated registered pair, now we need to set it to the correct store value
		writeBytes := dexkeeper.Cdc.MustMarshal(&registeredPair)
		rpStore.Set(pairPrefix, writeBytes)
	}

	// delete rpcnt
	rpcIterator := sdk.KVStorePrefixIterator(store, []byte(RegisteredPairCount))
	defer rpcIterator.Close()
	for ; rpcIterator.Valid(); rpcIterator.Next() {
		store.Delete(rpcIterator.Key())
	}

	// delete price Ticks
	priceTickIterator := sdk.KVStorePrefixIterator(store, []byte(PriceTickSizeKey))
	defer priceTickIterator.Close()
	for ; priceTickIterator.Valid(); priceTickIterator.Next() {
		store.Delete(priceTickIterator.Key())
	}

	// delete quantityTicks
	quantityTickIterator := sdk.KVStorePrefixIterator(store, []byte(QuantityTickSizeKey))
	defer quantityTickIterator.Close()
	for ; quantityTickIterator.Valid(); quantityTickIterator.Next() {
		store.Delete(quantityTickIterator.Key())
	}
	return nil
}

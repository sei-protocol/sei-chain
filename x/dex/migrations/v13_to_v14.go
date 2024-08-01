package migrations

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func V13ToV14(ctx sdk.Context, dexkeeper keeper.Keeper) error {
	setDefaultParams(ctx, dexkeeper)

	for _, contractAddr := range getAllContractAddresses(ctx, dexkeeper) {
		convertOrderBookEntryKeysForContract(ctx, dexkeeper, contractAddr)
	}
	return nil
}

func setDefaultParams(ctx sdk.Context, dexkeeper keeper.Keeper) {
	// This isn't the cleanest migration since it could potentially revert any dex params we have changed
	// but we haven't, so we'll just do this.
	defaultParams := types.DefaultParams()
	dexkeeper.SetParams(ctx, defaultParams)
}

func getAllContractAddresses(ctx sdk.Context, dexkeeper keeper.Keeper) []string {
	contracts := dexkeeper.GetAllContractInfo(ctx)
	return utils.Map(contracts, func(c types.ContractInfoV2) string { return c.ContractAddr })
}

func convertOrderBookEntryKeysForContract(ctx sdk.Context, dexkeeper keeper.Keeper, contractAddr string) {
	ctx.Logger().Info(fmt.Sprintf("converting order book entry keys for contract %s", contractAddr))
	store := prefix.NewStore(ctx.KVStore(dexkeeper.GetStoreKey()), types.ContractKeyPrefix(types.LongBookKey, contractAddr))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	keyToVal := map[string][]byte{}
	for ; iterator.Valid(); iterator.Next() {
		keyToVal[string(iterator.Key())] = iterator.Value()
	}
	iterator.Close()

	for key, v := range keyToVal {
		store.Delete([]byte(key))
		var val types.LongBook
		dexkeeper.Cdc.MustUnmarshal(v, &val)
		newKey := append(types.PairPrefix(val.Entry.PriceDenom, val.Entry.AssetDenom), keeper.GetKeyForPrice(val.Price)...)
		store.Set(newKey, v)
	}

	store = prefix.NewStore(ctx.KVStore(dexkeeper.GetStoreKey()), types.ContractKeyPrefix(types.ShortBookKey, contractAddr))
	iterator = sdk.KVStorePrefixIterator(store, []byte{})

	keyToVal = map[string][]byte{}
	for ; iterator.Valid(); iterator.Next() {
		keyToVal[string(iterator.Key())] = iterator.Value()
	}
	iterator.Close()

	for key, v := range keyToVal {
		store.Delete([]byte(key))
		var val types.ShortBook
		dexkeeper.Cdc.MustUnmarshal(v, &val)
		newKey := append(types.PairPrefix(val.Entry.PriceDenom, val.Entry.AssetDenom), keeper.GetKeyForPrice(val.Price)...)
		store.Set(newKey, v)
	}
}

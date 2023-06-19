package migrations

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

var DexPrefixes = []string{
	types.LongBookKey,
	types.ShortBookKey,
	"TriggerBook-value-",
	types.OrderKey,
	types.TwapKey,
	types.RegisteredPairKey,
	types.OrderKey,
	types.CancelKey,
	types.AccountActiveOrdersKey,
	types.NextOrderIDKey,
	types.MatchResultKey,
	types.MemOrderKey,
	types.MemCancelKey,
	types.MemDepositKey,
	types.PriceKey,
	"SettlementEntry-",
	"NextSettlementID-",
}

func V10ToV11(ctx sdk.Context, dexkeeper keeper.Keeper) error {
	dexkeeper.CreateModuleAccount(ctx)

	// this will nuke all old prefixes data in the store
	for _, prefixKey := range DexPrefixes {
		store := prefix.NewStore(ctx.KVStore(dexkeeper.GetStoreKey()), []byte(prefixKey))
		iterator := sdk.KVStorePrefixIterator(store, []byte{})

		defer iterator.Close()
		for ; iterator.Valid(); iterator.Next() {
			store.Delete(iterator.Key())
		}
	}

	return nil
}

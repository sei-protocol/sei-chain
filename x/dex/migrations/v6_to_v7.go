package migrations

import (
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func V6ToV7(ctx sdk.Context, storeKey sdk.StoreKey) error {
	backfillOrderIDPerContract(ctx, storeKey)
	reformatPriceState(ctx, storeKey)
	return nil
}

// this function backfills contract order ID according to the old global order ID
func backfillOrderIDPerContract(ctx sdk.Context, storeKey sdk.StoreKey) {
	oldStore := prefix.NewStore(
		ctx.KVStore(storeKey),
		[]byte{},
	)
	oldKey := types.KeyPrefix(types.NextOrderIDKey)
	oldIDBytes := oldStore.Get(oldKey)
	if oldIDBytes == nil {
		// nothing to backfill
		return
	}
	oldID := binary.BigEndian.Uint64(oldIDBytes)

	contractStore := prefix.NewStore(ctx.KVStore(storeKey), []byte(keeper.ContractPrefixKey))
	iterator := sdk.KVStorePrefixIterator(contractStore, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		contract := types.ContractInfo{}
		if err := contract.Unmarshal(iterator.Value()); err == nil {
			if contract.NeedOrderMatching {
				newIDStore := prefix.NewStore(ctx.KVStore(storeKey), types.NextOrderIDPrefix(contract.ContractAddr))
				byteKey := types.KeyPrefix(types.NextOrderIDKey)
				bz := make([]byte, 8)
				binary.BigEndian.PutUint64(bz, oldID)
				newIDStore.Set(byteKey, bz)
			}
		}
	}
}

func reformatPriceState(ctx sdk.Context, storeKey sdk.StoreKey) {
	contractStore := prefix.NewStore(ctx.KVStore(storeKey), []byte(keeper.ContractPrefixKey))
	iterator := sdk.KVStorePrefixIterator(contractStore, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		contract := types.ContractInfo{}
		if err := contract.Unmarshal(iterator.Value()); err == nil {
			pairStore := prefix.NewStore(ctx.KVStore(storeKey), types.RegisteredPairPrefix(contract.ContractAddr))
			pairIterator := sdk.KVStorePrefixIterator(pairStore, []byte{})
			for ; pairIterator.Valid(); pairIterator.Next() {
				pair := types.Pair{}
				if err := pair.Unmarshal(pairIterator.Value()); err == nil {
					oldPriceStore := prefix.NewStore(ctx.KVStore(storeKey), append(
						append(
							append(types.KeyPrefix(types.PriceKey), types.KeyPrefix(contract.ContractAddr)...),
							types.KeyPrefix(pair.PriceDenom)...,
						),
						types.KeyPrefix(pair.AssetDenom)...,
					))
					newPriceStore := prefix.NewStore(ctx.KVStore(storeKey), types.PricePrefix(contract.ContractAddr, pair.PriceDenom, pair.AssetDenom))
					oldPriceIterator := sdk.KVStorePrefixIterator(oldPriceStore, []byte{})
					for ; oldPriceIterator.Valid(); oldPriceIterator.Next() {
						newPriceStore.Set(oldPriceIterator.Key(), oldPriceIterator.Value())
					}
					oldPriceIterator.Close()
				}
			}

			pairIterator.Close()
		}
	}
}

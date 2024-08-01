package migrations

import (
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func V7ToV8(ctx sdk.Context, storeKey sdk.StoreKey) error {
	return flattenSettlements(ctx, storeKey)
}

func flattenSettlements(ctx sdk.Context, storeKey sdk.StoreKey) error {
	contractStore := prefix.NewStore(ctx.KVStore(storeKey), []byte(keeper.ContractPrefixKey))
	iterator := sdk.KVStorePrefixIterator(contractStore, []byte{})

	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		contract := types.ContractInfo{}
		if err := contract.Unmarshal(iterator.Value()); err != nil {
			return err
		}
		pairStore := prefix.NewStore(ctx.KVStore(storeKey), types.RegisteredPairPrefix(contract.ContractAddr))
		pairIterator := sdk.KVStorePrefixIterator(pairStore, []byte{})
		for ; pairIterator.Valid(); pairIterator.Next() {
			pair := types.Pair{}
			if err := pair.Unmarshal(pairIterator.Value()); err != nil {
				pairIterator.Close()
				return err
			}
			settlementStore := prefix.NewStore(
				ctx.KVStore(storeKey),
				SettlementEntryPrefix(contract.ContractAddr, pair.PriceDenom, pair.AssetDenom),
			)
			settlementIterator := sdk.KVStorePrefixIterator(settlementStore, []byte{})

			oldKeys := [][]byte{}
			newKeys := [][]byte{}
			newVals := [][]byte{}
			for ; settlementIterator.Valid(); settlementIterator.Next() {
				var val types.Settlements
				if err := val.Unmarshal(settlementIterator.Value()); err != nil {
					pairIterator.Close()
					settlementIterator.Close()
					return err
				}
				for i, settlementEntry := range val.Entries {
					settlementBytes, err := settlementEntry.Marshal()
					if err != nil {
						pairIterator.Close()
						settlementIterator.Close()
						return err
					}
					newKeys = append(newKeys, types.GetSettlementKey(settlementEntry.OrderId, settlementEntry.Account, uint64(i)))
					newVals = append(newVals, settlementBytes)
				}

				if len(val.Entries) > 0 {
					settlementIDStore := prefix.NewStore(
						ctx.KVStore(storeKey),
						NextSettlementIDPrefix(contract.ContractAddr, pair.PriceDenom, pair.AssetDenom),
					)
					key := make([]byte, 8)
					binary.BigEndian.PutUint64(key, val.Entries[0].OrderId)
					value := make([]byte, 8)
					binary.BigEndian.PutUint64(value, uint64(len(val.Entries)))
					settlementIDStore.Set(key, value)
				}
				oldKeys = append(oldKeys, settlementIterator.Key())
			}

			settlementIterator.Close()

			for _, oldKey := range oldKeys {
				settlementStore.Delete(oldKey)
			}
			for i, newKey := range newKeys {
				settlementStore.Set(newKey, newVals[i])
			}
		}
		pairIterator.Close()
	}
	return nil
}

func SettlementEntryPrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	return append(
		append(types.KeyPrefix("SettlementEntry-"), types.AddressKeyPrefix(contractAddr)...),
		types.PairPrefix(priceDenom, assetDenom)...,
	)
}

func NextSettlementIDPrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	return append(
		append(types.KeyPrefix("NextSettlementID-"), types.AddressKeyPrefix(contractAddr)...),
		types.PairPrefix(priceDenom, assetDenom)...,
	)
}

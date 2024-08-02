package contract

import (
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

var DexWhitelistedKeys = []string{
	types.LongBookKey,
	types.ShortBookKey,
	types.OrderKey,
	types.AccountActiveOrdersKey,
	types.CancelKey,
	types.TwapKey,
	types.PriceKey,
	types.NextOrderIDKey,
	types.MatchResultKey,
	types.LongOrderCountKey,
	types.ShortOrderCountKey,
	keeper.ContractPrefixKey,
}

var DexMemWhitelistedKeys = []string{
	types.MemOrderKey,
	types.MemDepositKey,
	types.MemCancelKey,
}

var WasmWhitelistedKeys = []string{
	string(wasmtypes.ContractStorePrefix),
}

func GetWhitelistMap(contractAddr string) map[string][]string {
	res := map[string][]string{}
	res[storetypes.NewKVStoreKey(types.StoreKey).Name()] = GetDexWhitelistedPrefixes(contractAddr)
	res[storetypes.NewKVStoreKey(wasmtypes.StoreKey).Name()] = GetWasmWhitelistedPrefixes(contractAddr)
	res[storetypes.NewKVStoreKey(types.MemStoreKey).Name()] = GetDexMemWhitelistedPrefixes(contractAddr)
	return res
}

func GetPerPairWhitelistMap(contractAddr string, pair types.Pair) map[string][]string {
	res := map[string][]string{}
	res[storetypes.NewKVStoreKey(types.StoreKey).Name()] = GetDexPerPairWhitelistedPrefixes(contractAddr, pair)
	res[storetypes.NewKVStoreKey(types.MemStoreKey).Name()] = GetDexMemPerPairWhitelistedPrefixes(contractAddr, pair)
	return res
}

func GetDexWhitelistedPrefixes(contractAddr string) []string {
	return utils.Map(DexWhitelistedKeys, func(key string) string {
		return string(append(
			types.KeyPrefix(key), types.AddressKeyPrefix(contractAddr)...,
		))
	})
}

func GetDexMemWhitelistedPrefixes(contractAddr string) []string {
	return utils.Map(DexMemWhitelistedKeys, func(key string) string {
		return string(append(
			types.KeyPrefix(key), types.AddressKeyPrefix(contractAddr)...,
		))
	})
}

func GetWasmWhitelistedPrefixes(contractAddr string) []string {
	addr, _ := sdk.AccAddressFromBech32(contractAddr)
	return utils.Map(WasmWhitelistedKeys, func(key string) string {
		return string(append(
			[]byte(key), addr...,
		))
	})
}

func GetDexPerPairWhitelistedPrefixes(contractAddr string, pair types.Pair) []string {
	return utils.Map(DexWhitelistedKeys, func(key string) string {
		return string(append(append(
			types.KeyPrefix(key), types.AddressKeyPrefix(contractAddr)...,
		), types.PairPrefix(pair.PriceDenom, pair.AssetDenom)...))
	})
}

func GetDexMemPerPairWhitelistedPrefixes(contractAddr string, pair types.Pair) []string {
	return utils.Map(DexMemWhitelistedKeys, func(key string) string {
		return string(append(append(
			types.KeyPrefix(key), types.AddressKeyPrefix(contractAddr)...,
		), types.PairPrefix(pair.PriceDenom, pair.AssetDenom)...))
	})
}

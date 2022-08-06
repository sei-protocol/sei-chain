package contract

import (
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
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
	types.SettlementEntryKey,
	types.NextOrderIDKey,
}

var WasmWhitelistedKeys = []string{
	string(wasmtypes.ContractStorePrefix),
}

func GetWhitelistMap(contractAddr string) map[string][]string {
	res := map[string][]string{}
	res[storetypes.NewKVStoreKey(types.StoreKey).Name()] = GetDexWhitelistedPrefixes(contractAddr)
	res[storetypes.NewKVStoreKey(wasmtypes.StoreKey).Name()] = GetWasmWhitelistedPrefixes(contractAddr)
	return res
}

func GetDexWhitelistedPrefixes(contractAddr string) []string {
	return utils.Map(DexWhitelistedKeys, func(key string) string {
		return string(append(
			types.KeyPrefix(key), types.KeyPrefix(contractAddr)...,
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

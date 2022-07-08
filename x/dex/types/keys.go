package types

import "encoding/binary"

const (
	// ModuleName defines the module name
	ModuleName = "dex"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey is the message route for slashing
	RouterKey = ModuleName

	// QuerierRoute defines the module's query routing key
	QuerierRoute = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_dex"
)

func KeyPrefix(p string) []byte {
	return []byte(p)
}

func ContractKeyPrefix(p string, contractAddr string) []byte {
	return append([]byte(p), []byte(contractAddr)...)
}

func PairPrefix(priceDenom string, assetDenom string) []byte {
	return append(KeyPrefix(priceDenom), KeyPrefix(assetDenom)...)
}

func OrderBookPrefix(long bool, contractAddr string, priceDenom string, assetDenom string) []byte {
	var prefix []byte
	if long {
		prefix = KeyPrefix(LongBookKey)
	} else {
		prefix = KeyPrefix(ShortBookKey)
	}
	return append(
		append(prefix, KeyPrefix(contractAddr)...),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

func TwapPrefix(contractAddr string) []byte {
	return append(KeyPrefix(TwapKey), KeyPrefix(contractAddr)...)
}

func PricePrefix(contractAddr string) []byte {
	return append(KeyPrefix(PriceKey), KeyPrefix(contractAddr)...)
}

func SettlementEntryPrefix(contractAddr string, blockHeight uint64) []byte {
	return append(
		append(KeyPrefix(SettlementEntryKey), KeyPrefix(contractAddr)...),
		GetKeyForHeight(blockHeight)...,
	)
}

func GetKeyForHeight(height uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, height)
	return key
}

func RegisteredPairPrefix(contractAddr string) []byte {
	return append(KeyPrefix(RegisteredPairKey), KeyPrefix(contractAddr)...)
}

func TickSizeKeyPrefix(contractAddr string) []byte {
	return append(KeyPrefix(TickSizeKey), KeyPrefix(contractAddr)...)
}

func RegisteredPairCountPrefix() []byte {
	return KeyPrefix(RegisteredPairCount)
}

func AssetListPrefix(assetDenom string) []byte {
	return append(KeyPrefix(AssetListKey), KeyPrefix(assetDenom)...)
}

const (
	DefaultPriceDenom = "stake"
	DefaultAssetDenom = "dummy"
)

const (
	LongBookKey      = "LongBook-value-"
	LongBookCountKey = "LongBook-count-"
)

const (
	ShortBookKey      = "ShortBook-value-"
	ShortBookCountKey = "ShortBook-count-"
)



const (
	TwapKey = "TWAP-"
    PriceKey = "Price-"
    SettlementEntryKey = "SettlementEntry-"
    NextOrderIdKey = "noid"
	RegisteredPairKey   = "rp"
	RegisteredPairCount = "rpcnt"
	TickSizeKey         = "ticks"
	AssetListKey = "AssetList-"
)

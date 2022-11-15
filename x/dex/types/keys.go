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

	// We don't want pair ABC<>DEF to have the same key as AB<>CDEF
	PairSeparator = "|"
)

func KeyPrefix(p string) []byte {
	return []byte(p)
}

func ContractKeyPrefix(p string, contractAddr string) []byte {
	return append([]byte(p), []byte(contractAddr)...)
}

func PairPrefix(priceDenom string, assetDenom string) []byte {
	return append([]byte(priceDenom), append([]byte(PairSeparator), []byte(assetDenom)...)...)
}

func LimitBookPrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	prefix := KeyPrefix(LimitBookKey)

	return append(
		append(prefix, KeyPrefix(contractAddr)...),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

func MarketBookPrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	prefix := KeyPrefix(MarketBookKey)

	return append(
		append(prefix, KeyPrefix(contractAddr)...),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

func CancelBookPrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	prefix := KeyPrefix(CancelBookKey)

	return append(
		append(prefix, KeyPrefix(contractAddr)...),
		PairPrefix(priceDenom, assetDenom)...,
	)
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

func TriggerOrderBookPrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	prefix := KeyPrefix(TriggerBookKey)

	return append(
		append(prefix, KeyPrefix(contractAddr)...),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

func TwapPrefix(contractAddr string) []byte {
	return append(KeyPrefix(TwapKey), KeyPrefix(contractAddr)...)
}

// `Price` constant + contract + price denom + asset denom
func PricePrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	return append(
		append(KeyPrefix(PriceKey), KeyPrefix(contractAddr)...),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

func SettlementEntryPrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	return append(
		append(KeyPrefix(SettlementEntryKey), KeyPrefix(contractAddr)...),
		PairPrefix(priceDenom, assetDenom)...,
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

func Cancel(contractAddr string) []byte {
	return append(KeyPrefix(CancelKey), KeyPrefix(contractAddr)...)
}

func AccountActiveOrdersPrefix(contractAddr string) []byte {
	return append(KeyPrefix(AccountActiveOrdersKey), KeyPrefix(contractAddr)...)
}

func RegisteredPairCountPrefix() []byte {
	return KeyPrefix(RegisteredPairCount)
}

func AssetListPrefix(assetDenom string) []byte {
	return append(KeyPrefix(AssetListKey), KeyPrefix(assetDenom)...)
}

func NextOrderIDPrefix(contractAddr string) []byte {
	return append(KeyPrefix(NextOrderIDKey), KeyPrefix(contractAddr)...)
}

func NextSettlementIDPrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	return append(
		append(KeyPrefix(NextSettlementIDKey), KeyPrefix(contractAddr)...),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

func MatchResultPrefix(contractAddr string) []byte {
	return append(KeyPrefix(MatchResultKey), KeyPrefix(contractAddr)...)
}

func GetSettlementOrderIDPrefix(orderID uint64, account string) []byte {
	accountBytes := append([]byte(account), []byte("|")...)
	orderIDBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(orderIDBytes, orderID)
	return append(accountBytes, orderIDBytes...)
}

func GetSettlementKey(orderID uint64, account string, settlementID uint64) []byte {
	settlementIDBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(settlementIDBytes, settlementID)
	return append(GetSettlementOrderIDPrefix(orderID, account), settlementIDBytes...)
}

const (
	DefaultPriceDenom = "stake"
	DefaultAssetDenom = "dummy"
)

const (
	LongBookKey = "LongBook-value-"

	ShortBookKey = "ShortBook-value-"

	LimitBookKey   = "LimitBook-value-"
	MarketBookKey  = "MarketBook-value-"
	TriggerBookKey = "TriggerBook-value-"
	CancelBookKey = "CancelBook-value-"

	OrderKey               = "order"
	AccountActiveOrdersKey = "account-active-orders"
	CancelKey              = "cancel"

	TwapKey             = "TWAP-"
	PriceKey            = "Price-"
	SettlementEntryKey  = "SettlementEntry-"
	NextSettlementIDKey = "NextSettlementID-"
	NextOrderIDKey      = "noid"
	RegisteredPairKey   = "rp"
	RegisteredPairCount = "rpcnt"
	TickSizeKey         = "ticks"
	AssetListKey        = "AssetList-"
	MatchResultKey      = "MatchResult-"
)

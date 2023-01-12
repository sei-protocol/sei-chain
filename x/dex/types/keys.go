package types

import (
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

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
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append([]byte(p), address.Bytes()...)
}

func PairPrefix(priceDenom string, assetDenom string) []byte {
	return append([]byte(priceDenom), append([]byte(PairSeparator), []byte(assetDenom)...)...)
}

func OrderBookPrefix(long bool, contractAddr string, priceDenom string, assetDenom string) []byte {
	var prefix []byte
	if long {
		prefix = KeyPrefix(LongBookKey)
	} else {
		prefix = KeyPrefix(ShortBookKey)
	}
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(
		append(prefix, address.Bytes()...),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

func TriggerOrderBookPrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	prefix := KeyPrefix(TriggerBookKey)
	address, _ := sdk.AccAddressFromBech32(contractAddr)

	return append(
		append(prefix, address.Bytes()...),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

func TwapPrefix(contractAddr string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(KeyPrefix(TwapKey), address.Bytes()...)
}

// `Price` constant + contract + price denom + asset denom
func PricePrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(
		append(KeyPrefix(PriceKey), address.Bytes()...),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

func SettlementEntryPrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(
		append(KeyPrefix(SettlementEntryKey), address.Bytes()...),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

func GetKeyForHeight(height uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, height)
	return key
}

func RegisteredPairPrefix(contractAddr string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(KeyPrefix(RegisteredPairKey), address.Bytes()...)
}

func OrderPrefix(contractAddr string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(KeyPrefix(OrderKey), address.Bytes()...)
}

func Cancel(contractAddr string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(KeyPrefix(CancelKey), address.Bytes()...)
}

func AccountActiveOrdersPrefix(contractAddr string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(KeyPrefix(AccountActiveOrdersKey), address.Bytes()...)
}

func AssetListPrefix(assetDenom string) []byte {
	return append(KeyPrefix(AssetListKey), KeyPrefix(assetDenom)...)
}

func NextOrderIDPrefix(contractAddr string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(KeyPrefix(NextOrderIDKey), address.Bytes()...)
}

func NextSettlementIDPrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(
		append(KeyPrefix(NextSettlementIDKey), address.Bytes()...),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

func MatchResultPrefix(contractAddr string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(KeyPrefix(MatchResultKey), address.Bytes()...)
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

func MemOrderPrefixForPair(contractAddr string, pairString string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(
		append(KeyPrefix(MemOrderKey), address.Bytes()...),
		[]byte(pairString)...,
	)
}

func MemCancelPrefixForPair(contractAddr string, pairString string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(
		append(KeyPrefix(MemCancelKey), address.Bytes()...),
		[]byte(pairString)...,
	)
}

func MemOrderPrefix(contractAddr string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(KeyPrefix(MemOrderKey), address.Bytes()...)
}

func MemCancelPrefix(contractAddr string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(KeyPrefix(MemCancelKey), address.Bytes()...)
}

func MemDepositPrefix(contractAddr string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return append(KeyPrefix(MemDepositKey), address.Bytes()...)
}

func MemDepositSubprefix(creator, denom string) []byte {
	return append([]byte(creator), []byte(denom)...)
}

func ContractKey(contractAddr string) []byte {
	address, _ := sdk.AccAddressFromBech32(contractAddr)
	return address.Bytes()
}

const (
	DefaultPriceDenom = "usei"
	DefaultAssetDenom = "dummy"
)

const (
	LongBookKey = "LongBook-value-"

	ShortBookKey = "ShortBook-value-"

	TriggerBookKey = "TriggerBook-value-"

	OrderKey               = "order"
	AccountActiveOrdersKey = "account-active-orders"
	CancelKey              = "cancel"

	TwapKey             = "TWAP-"
	PriceKey            = "Price-"
	SettlementEntryKey  = "SettlementEntry-"
	NextSettlementIDKey = "NextSettlementID-"
	NextOrderIDKey      = "noid"
	RegisteredPairKey   = "rp"
	AssetListKey        = "AssetList-"
	MatchResultKey      = "MatchResult-"

	MemOrderKey   = "MemOrder-"
	MemDepositKey = "MemDeposit-"
	MemCancelKey  = "MemCancel-"
)

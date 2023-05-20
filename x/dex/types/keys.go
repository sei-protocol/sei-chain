package types

import (
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
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

func AddressKeyPrefix(contractAddr string) []byte {
	addr, _ := sdk.AccAddressFromBech32(contractAddr)
	return address.MustLengthPrefix(addr)
}

func ContractKeyPrefix(p string, contractAddr string) []byte {
	return append([]byte(p), AddressKeyPrefix(contractAddr)...)
}

func PairPrefix(priceDenom string, assetDenom string) []byte {
	return append([]byte(priceDenom), append([]byte(PairSeparator), []byte(assetDenom)...)...)
}

func OrderBookPrefix(long bool, contractAddr string, priceDenom string, assetDenom string) []byte {
	return append(
		OrderBookContractPrefix(long, contractAddr),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

func OrderBookContractPrefix(long bool, contractAddr string) []byte {
	var prefix []byte
	if long {
		prefix = KeyPrefix(LongBookKey)
	} else {
		prefix = KeyPrefix(ShortBookKey)
	}
	return append(prefix, AddressKeyPrefix(contractAddr)...)
}

func TriggerOrderBookPrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	prefix := KeyPrefix(TriggerBookKey)

	return append(
		append(prefix, AddressKeyPrefix(contractAddr)...),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

// `Price` constant + contract + price denom + asset denom
func PricePrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	return append(
		PriceContractPrefix(contractAddr),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

func PriceContractPrefix(contractAddr string) []byte {
	return append(KeyPrefix(PriceKey), AddressKeyPrefix(contractAddr)...)
}

func SettlementEntryPrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	return append(
		append(KeyPrefix(SettlementEntryKey), AddressKeyPrefix(contractAddr)...),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

func RegisteredPairPrefix(contractAddr string) []byte {
	return append(KeyPrefix(RegisteredPairKey), AddressKeyPrefix(contractAddr)...)
}

func OrderPrefix(contractAddr string) []byte {
	return append(KeyPrefix(OrderKey), AddressKeyPrefix(contractAddr)...)
}

func AssetListPrefix(assetDenom string) []byte {
	return append(KeyPrefix(AssetListKey), KeyPrefix(assetDenom)...)
}

func NextOrderIDPrefix(contractAddr string) []byte {
	return append(KeyPrefix(NextOrderIDKey), AddressKeyPrefix(contractAddr)...)
}

func NextSettlementIDPrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	return append(
		append(KeyPrefix(NextSettlementIDKey), AddressKeyPrefix(contractAddr)...),
		PairPrefix(priceDenom, assetDenom)...,
	)
}

func MatchResultPrefix(contractAddr string) []byte {
	return append(KeyPrefix(MatchResultKey), AddressKeyPrefix(contractAddr)...)
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
	return append(
		append(KeyPrefix(MemOrderKey), AddressKeyPrefix(contractAddr)...),
		[]byte(pairString)...,
	)
}

func MemCancelPrefixForPair(contractAddr string, pairString string) []byte {
	return append(
		append(KeyPrefix(MemCancelKey), AddressKeyPrefix(contractAddr)...),
		[]byte(pairString)...,
	)
}

func MemOrderPrefix(contractAddr string) []byte {
	return append(KeyPrefix(MemOrderKey), AddressKeyPrefix(contractAddr)...)
}

func MemCancelPrefix(contractAddr string) []byte {
	return append(KeyPrefix(MemCancelKey), AddressKeyPrefix(contractAddr)...)
}

func MemDepositPrefix(contractAddr string) []byte {
	return append(KeyPrefix(MemDepositKey), AddressKeyPrefix(contractAddr)...)
}

func MemDepositSubprefix(creator, denom string) []byte {
	return append([]byte(creator), []byte(denom)...)
}

func ContractKey(contractAddr string) []byte {
	return AddressKeyPrefix(contractAddr)
}

func OrderCountPrefix(contractAddr string, priceDenom string, assetDenom string, long bool) []byte {
	var prefix []byte
	if long {
		prefix = KeyPrefix(LongOrderCountKey)
	} else {
		prefix = KeyPrefix(ShortOrderCountKey)
	}
	return append(prefix, append(AddressKeyPrefix(contractAddr), PairPrefix(priceDenom, assetDenom)...)...)
}

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
	LongOrderCountKey   = "loc-"
	ShortOrderCountKey  = "soc-"

	MemOrderKey   = "MemOrder-"
	MemDepositKey = "MemDeposit-"
	MemCancelKey  = "MemCancel-"
)

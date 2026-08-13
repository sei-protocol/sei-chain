package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
	"github.com/cosmos/cosmos-sdk/types/kv"
)

const (
	// ModuleName defines the module name
	ModuleName = "bank"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// DeferredCacheStoreKey defines the store key for the deferred cache
	DeferredCacheStoreKey = "deferredcache"

	// RouterKey defines the module's message routing key
	RouterKey = ModuleName

	// QuerierRoute defines the module's query routing key
	QuerierRoute = ModuleName
)

// KVStore keys
var (
	WeiBalancesPrefix = []byte{0x04}
	// BalancesPrefix is the prefix for the account balances store. We use a byte
	// (instead of `[]byte("balances")` to save some disk space).
	DeferredCachePrefix  = []byte{0x03}
	BalancesPrefix       = []byte{0x02}
	SupplyKey            = []byte{0x00}
	DenomMetadataPrefix  = []byte{0x1}
	DenomAllowListPrefix = []byte{0x11}
)

// DenomMetadataKey returns the denomination metadata key.
func DenomMetadataKey(denom string) []byte {
	d := []byte(denom)
	return append(DenomMetadataPrefix, d...)
}

// DenomAllowListKey returns the denomination allow list key.
func DenomAllowListKey(denom string) []byte {
	d := []byte(denom)
	return append(DenomAllowListPrefix, d...)
}

// AddressFromBalancesStore returns an account address from a balances prefix
// store. The key must not contain the prefix BalancesPrefix as the prefix store
// iterator discards the actual prefix.
//
// If invalid key is passed, AddressFromBalancesStore returns ErrInvalidKey.
func AddressFromBalancesStore(key []byte) (sdk.AccAddress, error) {
	if len(key) == 0 {
		return nil, ErrInvalidKey
	}
	kv.AssertKeyAtLeastLength(key, 1)
	addrLen := key[0]
	bound := int(addrLen)
	if len(key)-1 < bound {
		return nil, ErrInvalidKey
	}
	return key[1 : bound+1], nil
}

// CreateAccountBalancesPrefix creates the prefix for an account's balances.
func CreateAccountBalancesPrefix(addr []byte) []byte {
	return append(BalancesPrefix, address.MustLengthPrefix(addr)...)
}

func CreateAccountBalancesPrefixFromBech32(addr string) []byte {
	accAdrr, _ := sdk.AccAddressFromBech32(addr)
	accAdrrPrefix := CreateAccountBalancesPrefix(accAdrr)
	return accAdrrPrefix
}

// CreatePrefixedAccountStoreKey returns the key for the given account and denomination.
// This method can be used when performing an ABCI query for the balance of an account.
func CreatePrefixedAccountStoreKey(addr []byte, denom []byte) []byte {
	return append(CreateAccountBalancesPrefix(addr), denom...)
}

// This creates the prefix for use for the mem KV store used to track deferred balances by module name
func CreateDeferredCacheModulePrefix(moduleAddr []byte) []byte {
	return append(DeferredCachePrefix, address.MustLengthPrefix(moduleAddr)...)
}

// This creates the prefix for use for the mem KV store used to track deferred balances by module and txIndex to appropriately partition reads and writes to and from module balances
func CreateDeferredCacheModuleTxIndexedPrefix(moduleAddr []byte, index uint64) []byte {
	return append(CreateDeferredCacheModulePrefix(moduleAddr), sdk.Uint64ToBigEndian(index)...)
}

// AddressFromDeferredCacheStore returns an account address from a deferred Cache prefix
// store. The key must not contain the prefix DeferredCachePrefix as the prefix store
// iterator discards the actual prefix.
//
// If invalid key is passed, AddressFromBalancesStore returns ErrInvalidKey.
func AddressFromDeferredCacheStore(key []byte) (sdk.AccAddress, error) {
	if len(key) == 0 {
		return nil, ErrInvalidKey
	}
	kv.AssertKeyAtLeastLength(key, 1)
	addrLen := key[0]
	bound := int(addrLen)
	if len(key)-1 < bound {
		return nil, ErrInvalidKey
	}
	return key[1 : bound+1], nil
}

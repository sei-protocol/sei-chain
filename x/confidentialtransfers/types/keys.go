package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
)

// TODO: Remove keys that are eventually not required
const (
	// ModuleName defines the module name
	ModuleName = "confidentialtransfers"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey is the message route for slashing
	RouterKey = ModuleName

	// QuerierRoute defines the module's query routing key
	QuerierRoute = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_confidential"
)
const KeySeparator = "|"

var (
	AccountsKey = []byte{0x01}
	DenomKey    = []byte{0x02}
)

// GetAccountKey generates the key for storing Account information based on address and denom
func GetAccountKey(addr sdk.AccAddress, denom string) []byte {
	return append(GetAddressPrefix(addr), []byte(denom)...)
}

// GetAddressPrefix generates the prefix for all accounts under a specific address
func GetAddressPrefix(addr sdk.AccAddress) []byte {
	return append(AccountsKey, address.MustLengthPrefix(addr)...)
}

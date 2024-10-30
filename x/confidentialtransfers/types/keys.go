package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"strings"
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
	AccountsKey = "account"
	DenomKey    = []byte("denom")
)

// GetAccountKey generates the key for storing Account information based on address and denom
// The key is of the form: account | <addr> | <denom>
func GetAccountKey(address sdk.AccAddress, denom string) []byte {
	return []byte(strings.Join([]string{AccountsKey, address.String(), denom, ""}, KeySeparator))
}

// GetAddressPrefix generates the prefix for all accounts under a specific address
// The prefix is of the form: account|<addr>|
func GetAddressPrefix(address sdk.AccAddress) []byte {
	return []byte(strings.Join([]string{AccountsKey, address.String(), ""}, KeySeparator))
}

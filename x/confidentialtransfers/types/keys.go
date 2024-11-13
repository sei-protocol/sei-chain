package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
)

const (
	// ModuleName defines the module name
	ModuleName = "confidentialtransfers"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey is the message route for slashing
	RouterKey = ModuleName

	// QuerierRoute defines the module's query routing key
	QuerierRoute = ModuleName
)

var (
	AccountsKey = []byte{0x01}
)

// GetAddressPrefix generates the prefix for all accounts under a specific address
func GetAddressPrefix(addr sdk.AccAddress) []byte {
	return append(AccountsKey, address.MustLengthPrefix(addr)...)
}

package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
)

const (
	// ModuleName defines the module name
	ModuleName = "confidentialtransfers"

	ShortModuleName = "ct"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey is the message route for slashing
	RouterKey = ModuleName

	// QuerierRoute defines the module's query routing key
	QuerierRoute = ModuleName
)

var (
	AccountsKeyPrefix = []byte{0x01}
	KeyEnableCtModule = []byte("EnableCtModule")
	KeyRangeProofGas  = []byte("RangeProofGasMultiplier")
)

// GetAddressPrefix generates the prefix for all accounts under a specific address
func GetAddressPrefix(addr sdk.AccAddress) []byte {
	return append(AccountsKeyPrefix, address.MustLengthPrefix(addr)...)
}

func GetAccountPrefixFromBech32(addr string) []byte {
	accAdrr, _ := sdk.AccAddressFromBech32(addr)
	accAdrrPrefix := GetAddressPrefix(accAdrr)
	return accAdrrPrefix
}

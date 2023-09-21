package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

const (
	// module name
	ModuleName = "evm"

	RouterKey = ModuleName

	// StoreKey is string representation of the store key for auth
	StoreKey = "evm"

	// QuerierRoute is the querier route for auth
	QuerierRoute = ModuleName
)

var (
	BalanceKeyPrefix                = []byte{0x01}
	EVMAddressToSeiAddressKeyPrefix = []byte{0x02}
	SeiAddressToEVMAddressKeyPrefix = []byte{0x02}
)

func BalanceKey(addr common.Address) []byte {
	return append(BalanceKeyPrefix, addr[:]...)
}

func EVMAddressToSeiAddressKey(evmAddress common.Address) []byte {
	return append(EVMAddressToSeiAddressKeyPrefix, evmAddress[:]...)
}

func SeiAddressToEVMAddressKey(seiAddress sdk.AccAddress) []byte {
	return append(SeiAddressToEVMAddressKeyPrefix, seiAddress...)
}

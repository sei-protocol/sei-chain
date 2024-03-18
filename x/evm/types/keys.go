package types

import (
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

const (
	// module name
	ModuleName = "evm"

	RouterKey = ModuleName

	// StoreKey is string representation of the store key for auth
	StoreKey = "evm"

	MemStoreKey = "evm_mem"

	// QuerierRoute is the querier route for auth
	QuerierRoute = ModuleName
)

var (
	EVMAddressToSeiAddressKeyPrefix            = []byte{0x01}
	SeiAddressToEVMAddressKeyPrefix            = []byte{0x02}
	StateKeyPrefix                             = []byte{0x03}
	TransientStateKeyPrefix                    = []byte{0x04} // deprecated
	AccountTransientStateKeyPrefix             = []byte{0x05} // deprecated
	TransientModuleStateKeyPrefix              = []byte{0x06} // deprecated
	CodeKeyPrefix                              = []byte{0x07}
	CodeHashKeyPrefix                          = []byte{0x08}
	CodeSizeKeyPrefix                          = []byte{0x09}
	NonceKeyPrefix                             = []byte{0x0a}
	ReceiptKeyPrefix                           = []byte{0x0b}
	WhitelistedCodeHashesForBankSendPrefix     = []byte{0x0c}
	BlockBloomPrefix                           = []byte{0x0d}
	TxHashesPrefix                             = []byte{0x0e}
	WhitelistedCodeHashesForDelegateCallPrefix = []byte{0x0f}
	//mem
	TxHashPrefix  = []byte{0x10}
	TxBloomPrefix = []byte{0x11}

	ReplaySeenAddrPrefix = []byte{0x12}
	ReplayedHeight       = []byte{0x13}
	ReplayInitialHeight  = []byte{0x14}
)

func EVMAddressToSeiAddressKey(evmAddress common.Address) []byte {
	return append(EVMAddressToSeiAddressKeyPrefix, evmAddress[:]...)
}

func SeiAddressToEVMAddressKey(seiAddress sdk.AccAddress) []byte {
	return append(SeiAddressToEVMAddressKeyPrefix, seiAddress...)
}

func StateKey(evmAddress common.Address) []byte {
	return append(StateKeyPrefix, evmAddress[:]...)
}

func ReceiptKey(txHash common.Hash) []byte {
	return append(ReceiptKeyPrefix, txHash[:]...)
}

func BlockBloomKey(height int64) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, uint64(height))
	return append(BlockBloomPrefix, bz...)
}

func TxHashesKey(height int64) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, uint64(height))
	return append(TxHashesPrefix, bz...)
}

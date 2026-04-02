package cryptosim

import (
	"encoding/hex"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
)

// BytesToHex returns a lowercase hex string with 0x prefix, suitable for printing binary keys or addresses.
func BytesToHex(b []byte) string {
	return "0x" + hex.EncodeToString(b)
}

// Get the key for the account ID counter in the database.
// Uses EVMKeyCode with padded keyBytes; EVMKeyNonce requires 20-byte addresses and
// non-standard lengths are routed to EVMKeyLegacy which FlatKV ignores.
func AccountIDCounterKey() []byte {
	return evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, paddedCounterKey(accountIdCounterKey))
}

// Get the key for the ERC20 contract ID counter in the database.
func Erc20IDCounterKey() []byte {
	return evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, paddedCounterKey(erc20IdCounterKey))
}

// Get the key for the block number counter in the database.
func BlockNumberCounterKey() []byte {
	return evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, paddedCounterKey(blockNumberCounterKey))
}

// paddedCounterKey pads the string to AddressLen bytes for use with EVM key builders.
func paddedCounterKey(s string) []byte {
	b := make([]byte, AddressLen)
	copy(b, s)
	return b
}

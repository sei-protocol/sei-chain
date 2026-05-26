// Package ktype defines the physical key encoding used by FlatKV's data DBs.
//
// Every key stored in accountDB, codeDB, storageDB, and legacyDB is prefixed
// with "moduleName/" so that keys remain unique and LtHash-stable when DBs are
// merged in the future.
package ktype

import (
	"bytes"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
)

// ---------------------------------------------------------------------------
// Domain types
// ---------------------------------------------------------------------------

const (
	AddressLen = 20
	SlotLen    = 32
)

// Address is an EVM address (20 bytes).
type Address [AddressLen]byte

// Slot is a storage slot key (32 bytes).
type Slot [SlotLen]byte

// StorageKey returns the stripped storageDB key for (addr, slot).
// Key format: addr(20) || slot(32) = 52 bytes.
// This is NOT the physical DB key; callers must wrap with EVMPhysicalKey.
func StorageKey(addr Address, slot Slot) []byte {
	key := make([]byte, AddressLen+SlotLen)
	copy(key[:AddressLen], addr[:])
	copy(key[AddressLen:], slot[:])
	return key
}

// ---------------------------------------------------------------------------
// Physical key encoding
// ---------------------------------------------------------------------------

// EVMKeyAccount is the canonical EVMKeyKind for the merged account row in
// accountDB. FlatKV merges nonce (0x0a), codehash (0x08), and future balance
// into one physical row. The nonce prefix byte (0x0a) is reused as the
// canonical type byte so the physical key is "evm/" + 0x0a + addr.
//
// Physical key prefix mapping (all DBs):
//
//	Kind            Prefix  DB         Physical Key
//	EVMKeyStorage   0x03    storageDB  "evm/" + 0x03 + addr||slot
//	EVMKeyAccount   0x0a    accountDB  "evm/" + 0x0a + addr  (merges nonce, codehash, balance)
//	EVMKeyCode      0x07    codeDB     "evm/" + 0x07 + addr
//	EVMKeyLegacy    (orig)  legacyDB   "evm/" + original_key  OR  "module/" + cosmos_key
const EVMKeyAccount = keys.EVMKeyNonce

// ModulePhysicalKey returns "moduleName/" + key.
// All four data DBs (account, code, storage, legacy) use this format so keys
// remain unique and LtHash-stable when DBs are merged in the future.
func ModulePhysicalKey(moduleName string, key []byte) []byte {
	n := len(moduleName)
	result := make([]byte, n+1+len(key))
	copy(result, moduleName)
	result[n] = '/'
	copy(result[n+1:], key)
	return result
}

// StripModulePrefix splits a module-prefixed physical key into its module name
// and original key. Returns an error if no "/" separator is found.
func StripModulePrefix(physicalKey []byte) (moduleName string, originalKey []byte, err error) {
	idx := bytes.IndexByte(physicalKey, '/')
	if idx < 0 {
		return "", nil, fmt.Errorf("physical key missing module prefix separator '/': %x", physicalKey)
	}
	return string(physicalKey[:idx]), physicalKey[idx+1:], nil
}

// EVMPhysicalKey returns the physical DB key for an EVM key kind.
// Format: "evm/" + type_prefix_byte + stripped_key.
// For account keys (nonce, codehash), canonicalizes to EVMKeyAccount (0x0a)
// because these fields are merged into one physical row.
func EVMPhysicalKey(kind keys.EVMKeyKind, strippedKey []byte) []byte {
	if kind == keys.EVMKeyCodeHash {
		kind = EVMKeyAccount
	}
	prefixByte, ok := keys.EVMKeyPrefixByte(kind)
	if !ok {
		return nil
	}
	mod := keys.EVMStoreKey
	result := make([]byte, len(mod)+2+len(strippedKey))
	copy(result, mod)
	result[len(mod)] = '/'
	result[len(mod)+1] = prefixByte
	copy(result[len(mod)+2:], strippedKey)
	return result
}

// StripEVMPhysicalKey extracts the EVM key kind and stripped key from a
// physical DB key. This is the inverse of EVMPhysicalKey for export paths.
// For account keys the returned kind is EVMKeyAccount (keys.EVMKeyNonce).
func StripEVMPhysicalKey(physicalKey []byte) (kind keys.EVMKeyKind, strippedKey []byte, err error) {
	_, innerKey, err := StripModulePrefix(physicalKey)
	if err != nil {
		return keys.EVMKeyEmpty, nil, fmt.Errorf("strip EVM physical key: %w", err)
	}
	kind, strippedKey = keys.ParseEVMKey(innerKey)
	if kind == keys.EVMKeyEmpty {
		return keys.EVMKeyEmpty, nil, fmt.Errorf("unrecognised EVM key kind in physical key: %x", physicalKey)
	}
	return kind, strippedKey, nil
}

// PrefixEnd returns the exclusive upper bound for prefix iteration (or nil).
func PrefixEnd(prefix []byte) []byte {
	if len(prefix) == 0 {
		return nil
	}
	b := bytes.Clone(prefix)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] != 0xFF {
			b[i]++
			return b[:i+1]
		}
	}
	return nil
}

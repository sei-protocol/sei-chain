// Package ktype defines the physical key encoding used by FlatKV's data DBs.
//
// Every key stored in accountDB, codeDB, storageDB, and miscDB is prefixed
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
//	EVMKeyMisc    (orig)  miscDB   "evm/" + original_key  OR  "module/" + cosmos_key
const EVMKeyAccount = keys.EVMKeyNonce

// MaxEVMPhysicalKeyLen is the length of the longest physical key EVMPhysicalKey can produce:
// "evm/" plus the type prefix byte plus a storage key (address || slot). Callers building EVM
// physical keys into a reusable buffer can size it from this and never grow.
const MaxEVMPhysicalKeyLen = len(keys.EVMStoreKey) + 2 + AddressLen + SlotLen

// ModulePhysicalKey returns "moduleName/" + key.
// All four data DBs (account, code, storage, misc) use this format so keys
// remain unique and LtHash-stable when DBs are merged in the future.
func ModulePhysicalKey(moduleName string, key []byte) []byte {
	return AppendModulePhysicalKey(make([]byte, 0, len(moduleName)+1+len(key)), moduleName, key)
}

// AppendModulePhysicalKey appends "moduleName/" + key to dst and returns the extended slice.
func AppendModulePhysicalKey(dst []byte, moduleName string, key []byte) []byte {
	dst = append(dst, moduleName...)
	dst = append(dst, '/')
	return append(dst, key...)
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

// EVMPhysicalKey returns the physical DB key for an EVM key kind, or nil for a kind that has no
// prefix byte (e.g. misc). Format: "evm/" + type_prefix_byte + stripped_key.
// For account keys (nonce, codehash), canonicalizes to EVMKeyAccount (0x0a)
// because these fields are merged into one physical row.
func EVMPhysicalKey(kind keys.EVMKeyKind, strippedKey []byte) []byte {
	buf := make([]byte, 0, len(keys.EVMStoreKey)+2+len(strippedKey))
	physicalKey := AppendEVMPhysicalKey(buf, kind, strippedKey)
	if len(physicalKey) == 0 {
		// AppendEVMPhysicalKey appends nothing when the kind has no prefix byte.
		return nil
	}
	return physicalKey
}

// AppendEVMPhysicalKey appends the physical DB key for an EVM key kind to dst and returns the
// extended slice. Nothing is appended for a kind that has no prefix byte (e.g. misc).
func AppendEVMPhysicalKey(dst []byte, kind keys.EVMKeyKind, strippedKey []byte) []byte {
	if kind == keys.EVMKeyCodeHash {
		kind = EVMKeyAccount
	}
	prefixByte, ok := keys.EVMKeyPrefixByte(kind)
	if !ok {
		return dst
	}
	dst = append(dst, keys.EVMStoreKey...)
	dst = append(dst, '/', prefixByte)
	return append(dst, strippedKey...)
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

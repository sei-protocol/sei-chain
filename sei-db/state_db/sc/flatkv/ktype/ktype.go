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
// This is NOT the physical DB key; callers must wrap with BuildPhysicalEVMKey.
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
// accountDB. FlatKV merges nonce (0x0a), codehash (0x08), and balance (0x21)
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

// BuildPhysicalKey returns "moduleName/" + key.
// All four data DBs (account, code, storage, misc) use this format so keys
// remain unique and LtHash-stable when DBs are merged in the future.
func BuildPhysicalKey(moduleName string, key []byte) []byte {
	n := len(moduleName)
	result := make([]byte, n+1+len(key))
	copy(result, moduleName)
	result[n] = '/'
	copy(result[n+1:], key)
	return result
}

// WritePhysicalKeyToBuffer writes the key BuildPhysicalKey() returns into buf, overwriting buf's contents and reusing
// its capacity, and returns it.
func WritePhysicalKeyToBuffer(
	// The buffer to write into. May be nil.
	buf []byte,
	// The module name that prefixes the key.
	moduleName string,
	// The bytes that follow "moduleName/".
	key []byte,
) []byte {
	buf = append(buf[:0], moduleName...)
	buf = append(buf, '/')
	return append(buf, key...)
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

// BuildPhysicalEVMKey returns the physical DB key for an EVM key kind.
// Format: "evm/" + type_prefix_byte + stripped_key.
// For account keys (nonce, codehash, balance), canonicalizes to EVMKeyAccount
// (0x0a) because these fields are merged into one physical row.
func BuildPhysicalEVMKey(kind keys.EVMKeyKind, strippedKey []byte) []byte {
	if kind == keys.EVMKeyCodeHash || kind == keys.EVMKeyBalance {
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

// StripPhysicalEVMKey extracts the EVM key kind and stripped key from a
// physical DB key. This is the inverse of BuildPhysicalEVMKey for export paths.
// For account keys the returned kind is EVMKeyAccount (keys.EVMKeyNonce).
func StripPhysicalEVMKey(physicalKey []byte) (kind keys.EVMKeyKind, strippedKey []byte, err error) {
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

// WritePhysicalEVMKeyToBuffer writes the key BuildPhysicalEVMKey() returns into buf, overwriting buf's contents and
// reusing its capacity, and returns it. Returns nil for a kind with no prefix byte.
func WritePhysicalEVMKeyToBuffer(
	// The buffer to write into. May be nil.
	buf []byte,
	// The kind of row the key addresses. A code hash or balance kind addresses the account row.
	kind keys.EVMKeyKind,
	// The key with its kind prefix byte removed, such as an address, or an address followed by a slot.
	strippedKey []byte,
) []byte {
	if kind == keys.EVMKeyCodeHash || kind == keys.EVMKeyBalance {
		kind = EVMKeyAccount
	}
	prefixByte, ok := keys.EVMKeyPrefixByte(kind)
	if !ok {
		return nil
	}
	buf = append(buf[:0], keys.EVMStoreKey...)
	buf = append(buf, '/', prefixByte)
	return append(buf, strippedKey...)
}

// ParseLogicalKey returns the kind of a logical key and the key with its kind prefix byte removed. A key in a non-EVM
// module, or an EVM key of misc kind, is keys.EVMKeyMisc and is returned unchanged. Returns an error for an empty
// module name or an empty EVM key.
func ParseLogicalKey(
	// The module the logical key belongs to, such as "evm" or "bank".
	moduleName string,
	// The key as the module addresses it. An EVM key begins with its kind prefix byte.
	logicalKey []byte,
) (kind keys.EVMKeyKind, strippedKey []byte, err error) {
	if moduleName != keys.EVMStoreKey {
		if moduleName == "" {
			// An empty module name would fold into "/"+key and later persist as the per-module meta
			// key "_meta/x:/hash", which ParseModuleLtHashKey rejects on reload, so a store that ever
			// commits one becomes permanently unopenable. Module names are never empty in normal
			// operation (Cosmos SDK's NewKVStoreKey panics on an empty name), so this only guards
			// malformed input.
			return keys.EVMKeyEmpty, nil, fmt.Errorf("empty module name")
		}
		return keys.EVMKeyMisc, logicalKey, nil
	}
	kind, strippedKey = keys.ParseEVMKey(logicalKey)
	if kind == keys.EVMKeyEmpty {
		return kind, nil, fmt.Errorf("empty EVM key")
	}
	return kind, strippedKey, nil
}

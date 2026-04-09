package flatkv

import (
	"bytes"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

const metaKeyPrefix = "_meta/"

const (
	metaVersion = metaKeyPrefix + "version"
	metaLtHash  = metaKeyPrefix + "hash"
)

var (
	metaKeyPrefixBytes = []byte(metaKeyPrefix)
	metaVersionKey     = []byte(metaVersion)
	metaLtHashKey      = []byte(metaLtHash)
)

// isMetaKey reports whether key is a per-DB internal metadata key (not user data).
//
// Safety: _meta/ keys are 10–13 bytes; the shortest user key is 20 bytes
// (an EVM address). Prefix collision would require an address starting with
// 0x5F6D657461 ("_meta") — probability ~2^-48 for random addresses and
// negligible even under CREATE2 brute-force. Legacy DB keys must not use
// the _meta/ prefix.
func isMetaKey(key []byte) bool {
	return bytes.HasPrefix(key, metaKeyPrefixBytes)
}

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
const EVMKeyAccount = evm.EVMKeyNonce

const (
	AddressLen = 20
	SlotLen    = 32
)

// LocalMeta stores per-DB version tracking metadata.
// Version is stored at _meta/version, LtHash at _meta/hash.
type LocalMeta struct {
	CommittedVersion int64          // Current committed version in this DB
	LtHash           *lthash.LtHash // nil for old format (version-only)
}

// Address is an EVM address (20 bytes).
type Address [AddressLen]byte

// Slot is a storage slot key (32 bytes).
type Slot [SlotLen]byte

func AddressFromBytes(b []byte) (Address, bool) {
	if len(b) != AddressLen {
		return Address{}, false
	}
	var a Address
	copy(a[:], b)
	return a, true
}

// =============================================================================
// DB Key Builders
// =============================================================================

// AccountKey returns the accountDB key for addr.
// Key format: addr(20)
func AccountKey(addr Address) []byte {
	return addr[:]
}

// StorageKey returns the storageDB key for (addr, slot).
// Key format: addr(20) || slot(32) = 52 bytes
func StorageKey(addr Address, slot Slot) []byte {
	key := make([]byte, AddressLen+SlotLen)
	copy(key[:AddressLen], addr[:])
	copy(key[AddressLen:], slot[:])
	return key
}

func SlotFromBytes(b []byte) (Slot, bool) {
	if len(b) != SlotLen {
		return Slot{}, false
	}
	var s Slot
	copy(s[:], b)
	return s, true
}

// =============================================================================
// Module Prefix Helpers (physical key namespacing for all DBs)
// =============================================================================

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
// and original key. Returns ok=false if no "/" separator is found.
func StripModulePrefix(physicalKey []byte) (moduleName string, originalKey []byte, ok bool) {
	mod, rest, found := bytes.Cut(physicalKey, []byte{'/'})
	if !found {
		return "", nil, false
	}
	return string(mod), rest, true
}

// EVMPhysicalKey returns the physical DB key for an EVM key kind.
// Format: "evm/" + type_prefix_byte + stripped_key.
// For account keys (nonce, codehash), canonicalizes to EVMKeyAccount (0x0a)
// because these fields are merged into one physical row.
func EVMPhysicalKey(kind evm.EVMKeyKind, strippedKey []byte) []byte {
	canonicalKind := kind
	if kind == evm.EVMKeyCodeHash {
		canonicalKind = EVMKeyAccount
	}
	memiavlKey := evm.BuildMemIAVLEVMKey(canonicalKind, strippedKey)
	if memiavlKey == nil {
		return nil
	}
	return ModulePhysicalKey(evm.EVMStoreKey, memiavlKey)
}

// StripEVMPhysicalKey extracts the EVM key kind and stripped key from a
// physical DB key. This is the inverse of EVMPhysicalKey for export paths.
// For account keys the returned kind is EVMKeyAccount (evm.EVMKeyNonce).
func StripEVMPhysicalKey(physicalKey []byte) (kind evm.EVMKeyKind, strippedKey []byte, ok bool) {
	_, memiavlKey, found := StripModulePrefix(physicalKey)
	if !found {
		return evm.EVMKeyEmpty, nil, false
	}
	kind, strippedKey = evm.ParseEVMKey(memiavlKey)
	return kind, strippedKey, kind != evm.EVMKeyEmpty
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

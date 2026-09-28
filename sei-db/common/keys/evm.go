package keys

// AddressLen is the length in bytes of an EVM address (20 bytes, eth-style).
// Exported so that other packages (e.g. common/rand, benchmarks) can share a
// single canonical definition instead of maintaining their own copies.
const AddressLen = 20

const slotLen = 32

// FlatKVStoreKey is the module name used when exporting/importing data from
// the FlatKV backend. Treated as a separate module in state-sync snapshots
// so that import routes data exclusively to FlatKV.
const FlatKVStoreKey = "flatkv"

// EVM key prefixes — mirrored from x/evm/types/keys.go.
// These are immutable on-disk format markers; changing them would break
// all existing state, so duplicating here is safe and avoids pulling in the
// heavy x/evm/types dependency (which transitively imports cosmos-sdk).
const (
	stateKeyPrefixByte    byte = 0x03
	codeKeyPrefixByte     byte = 0x07
	codeHashKeyPrefixByte byte = 0x08
	nonceKeyPrefixByte    byte = 0x0a
	balanceKeyPrefixByte  byte = 0x21
)

// evmKeyPrefixLen is the length of every EVM key prefix above.
const evmKeyPrefixLen = 1

var (
	stateKeyPrefix    = []byte{stateKeyPrefixByte}
	codeKeyPrefix     = []byte{codeKeyPrefixByte}
	codeHashKeyPrefix = []byte{codeHashKeyPrefixByte}
	nonceKeyPrefix    = []byte{nonceKeyPrefixByte}
	balanceKeyPrefix  = []byte{balanceKeyPrefixByte}
)

// StateKeyPrefix returns the storage state key prefix (0x03).
// Exported for callers that need the raw prefix (e.g. iterator bounds).
func StateKeyPrefix() []byte { return stateKeyPrefix }

// EVMKeyKind identifies an EVM key family.
type EVMKeyKind uint8

// These values are in-memory routing tags, renumbered whenever a kind is added. Writing one into a
// key, a value, or any other stored or wire format is forbidden.
const (
	EVMKeyEmpty    EVMKeyKind = iota // Returned only for zero-length keys
	EVMKeyNonce                      // Stripped key: 20-byte address
	EVMKeyCodeHash                   // Stripped key: 20-byte address
	EVMKeyBalance                    // Stripped key: 20-byte address
	EVMKeyCode                       // Stripped key: 20-byte address
	EVMKeyStorage                    // Stripped key: addr||slot (20+32 bytes)
	EVMKeyMisc                       // Full original key preserved (address mappings, codesize, etc.)
)

// EVMKeyKindCount is the number of EVMKeyKind values, for sizing an array indexed by kind. It assumes EVMKeyMisc is
// the last kind.
const EVMKeyKindCount = int(EVMKeyMisc) + 1

// ParseEVMKey parses an EVM key from the x/evm store keyspace.
//
// For optimized keys (nonce, code, codehash, storage, balance), keyBytes is the stripped key.
// For misc keys (all other EVM data including codesize), keyBytes is the full original key.
// Only returns EVMKeyEmpty for zero-length keys.
func ParseEVMKey(key []byte) (kind EVMKeyKind, keyBytes []byte) {
	if len(key) == 0 {
		return EVMKeyEmpty, nil
	}

	// Every prefix is a single byte, so the first byte alone names the family.
	switch key[0] {
	case nonceKeyPrefixByte:
		if len(key) != evmKeyPrefixLen+AddressLen {
			return EVMKeyMisc, key // Malformed but still EVM data
		}
		return EVMKeyNonce, key[evmKeyPrefixLen:]

	case codeHashKeyPrefixByte:
		if len(key) != evmKeyPrefixLen+AddressLen {
			return EVMKeyMisc, key
		}
		return EVMKeyCodeHash, key[evmKeyPrefixLen:]

	case codeKeyPrefixByte:
		if len(key) != evmKeyPrefixLen+AddressLen {
			return EVMKeyMisc, key
		}
		return EVMKeyCode, key[evmKeyPrefixLen:]

	case stateKeyPrefixByte:
		if len(key) != evmKeyPrefixLen+AddressLen+slotLen {
			return EVMKeyMisc, key
		}
		return EVMKeyStorage, key[evmKeyPrefixLen:]

	case balanceKeyPrefixByte:
		if len(key) != evmKeyPrefixLen+AddressLen {
			return EVMKeyMisc, key
		}
		return EVMKeyBalance, key[evmKeyPrefixLen:]
	}

	// All other EVM keys go to the misc store (address mappings, codesize, etc.)
	return EVMKeyMisc, key
}

// EVMKeyPrefixByte returns the single-byte on-disk prefix for a given key kind.
// Returns (0, false) for kinds that have no fixed prefix (e.g. EVMKeyMisc).
func EVMKeyPrefixByte(kind EVMKeyKind) (byte, bool) {
	switch kind {
	case EVMKeyStorage:
		return stateKeyPrefix[0], true
	case EVMKeyNonce:
		return nonceKeyPrefix[0], true
	case EVMKeyCodeHash:
		return codeHashKeyPrefix[0], true
	case EVMKeyCode:
		return codeKeyPrefix[0], true
	case EVMKeyBalance:
		return balanceKeyPrefix[0], true
	default:
		return 0, false
	}
}

// BuildEVMKey builds a memiavl key from internal bytes.
// This is the reverse of ParseEVMKey for optimized key types.
//
// NOTE: This is primarily used for tests and temporary compatibility.
// FlatKV stores data in internal format; this function converts back to
// memiavl format for Iterator/Exporter output.
func BuildEVMKey(kind EVMKeyKind, keyBytes []byte) []byte {
	prefix, ok := EVMKeyPrefixByte(kind)
	if !ok {
		return nil
	}
	result := make([]byte, 1+len(keyBytes))
	result[0] = prefix
	copy(result[1:], keyBytes)
	return result
}

// PutEVMKey writes kind's prefix and parts into dst, which must be exactly 1 + the total length of parts.
// It reports false and leaves dst untouched when kind has no prefix.
func PutEVMKey(dst []byte, kind EVMKeyKind, parts ...[]byte) bool {
	prefix, ok := EVMKeyPrefixByte(kind)
	if !ok {
		return false
	}
	total := 1
	for _, part := range parts {
		total += len(part)
	}
	if len(dst) != total {
		return false
	}
	dst[0] = prefix
	offset := 1
	for _, part := range parts {
		offset += copy(dst[offset:], part)
	}
	return true
}

// InternalKeyLen returns the expected internal key length for a given kind.
// Used for validation in Iterator and tests.
func InternalKeyLen(kind EVMKeyKind) int {
	switch kind {
	case EVMKeyStorage:
		return AddressLen + slotLen // 52 bytes
	case EVMKeyNonce, EVMKeyCodeHash, EVMKeyCode, EVMKeyBalance:
		return AddressLen // 20 bytes
	default:
		return 0
	}
}

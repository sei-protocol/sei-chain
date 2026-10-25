package kvrepair

import (
	"bytes"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
)

// ReadsAsAbsent reports whether value, stored at key in store, reads the same
// as an absent key. A nil value is absent. In the evm store, an all-zero
// storage slot, nonce, code hash, or balance and an empty code value read as
// absent; every other value, and every value in another store, does not.
func ReadsAsAbsent(store string, key, value []byte) bool {
	if value == nil {
		return true
	}
	if store != keys.EVMStoreKey {
		return false
	}
	kind, _ := keys.ParseEVMKey(key)
	switch kind {
	case keys.EVMKeyStorage, keys.EVMKeyNonce, keys.EVMKeyCodeHash, keys.EVMKeyBalance:
		return isAllZero(value)
	case keys.EVMKeyCode:
		// Bytecode of zero bytes is real code, so only an empty value is absent.
		return len(value) == 0
	default:
		return false
	}
}

// ValuesEqual reports whether a and b, both stored at key in store, read the
// same. A nil value is absent, and a value that ReadsAsAbsent equals absent.
func ValuesEqual(store string, key, a, b []byte) bool {
	aAbsent, bAbsent := ReadsAsAbsent(store, key, a), ReadsAsAbsent(store, key, b)
	if aAbsent || bAbsent {
		return aAbsent && bAbsent
	}
	return bytes.Equal(a, b)
}

func isAllZero(value []byte) bool {
	for _, b := range value {
		if b != 0 {
			return false
		}
	}
	return true
}

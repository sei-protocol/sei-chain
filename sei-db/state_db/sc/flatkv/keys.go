package flatkv

import (
	"bytes"
	"encoding/binary"
)

// Key is a type-safe FlatKV key. Use constructors (AccountKey, StorageKey, etc.)
// to create keys; raw []byte cannot be cast to Key.
// Zero value (Key{}) represents unbounded in iterator operations.
type Key struct {
	b []byte
}

func keyFromBytes(b []byte) Key {
	if len(b) == 0 {
		return Key{}
	}
	return Key{b: b}
}

func (k Key) isZero() bool  { return len(k.b) == 0 }
func (k Key) bytes() []byte { return k.b }

// Keyspace prefixes (binary layout, stable across versions).
//
//	account: 0x01 || addr(20) || field(1)
//	code:    0x02 || codehash(32)
//	storage: 0x03 || addr(20) || slotLen(u16be) || slot
//	raw:     0x7f || keyLen(u32be) || originalKey
const (
	pfxAccount byte = 0x01
	pfxCode    byte = 0x02
	pfxStorage byte = 0x03
	// pfxRaw is a fallback namespace for keys FlatKV does not model explicitly.
	// It allows storing unknown/original EVM KV entries without data loss.
	pfxRaw byte = 0x7f
)

// AccountField identifies a field within an account record.
type AccountField byte

const (
	accountFieldNonce    byte = 0x01
	accountFieldCodeHash byte = 0x02

	// AccountFieldNonce is the nonce (tx count) field.
	// Usage: flatkv.AccountKey(addr, flatkv.AccountFieldNonce)
	AccountFieldNonce AccountField = AccountField(accountFieldNonce)

	// AccountFieldCodeHash is the contract codehash field.
	// Usage: flatkv.AccountKey(addr, flatkv.AccountFieldCodeHash)
	AccountFieldCodeHash AccountField = AccountField(accountFieldCodeHash)
)

// AccountKey returns the key for an account field (nonce, codehash).
func AccountKey(addr []byte, field AccountField) Key {
	k := make([]byte, 0, 1+len(addr)+1)
	k = append(k, pfxAccount)
	k = append(k, addr...)
	k = append(k, byte(field))
	return keyFromBytes(k)
}

// CodeKey returns the key for contract bytecode (indexed by codehash).
func CodeKey(codeHash []byte) Key {
	k := make([]byte, 0, 1+len(codeHash))
	k = append(k, pfxCode)
	k = append(k, codeHash...)
	return keyFromBytes(k)
}

// StorageKey returns the key for a contract storage slot.
func StorageKey(addr []byte, slot []byte) Key {
	k := make([]byte, 0, 1+len(addr)+2+len(slot))
	k = append(k, pfxStorage)
	k = append(k, addr...)
	var sz [2]byte
	binary.BigEndian.PutUint16(sz[:], uint16(len(slot))) //nolint:gosec
	k = append(k, sz[:]...)
	k = append(k, slot...)
	return keyFromBytes(k)
}

// RawKey wraps an unrecognized key (fallback for unknown prefixes).
func RawKey(original []byte) Key {
	k := make([]byte, 0, 1+4+len(original))
	k = append(k, pfxRaw)
	var sz [4]byte
	binary.BigEndian.PutUint32(sz[:], uint32(len(original))) //nolint:gosec
	k = append(k, sz[:]...)
	k = append(k, original...)
	return keyFromBytes(k)
}

// StoragePrefixStart returns the start key for iterating a contract's storage.
//
//	start := StoragePrefixStart(addr)
//	end := PrefixEnd(start)
//	iter := store.Iterator(start, end)
//	iter.First()
func StoragePrefixStart(addr []byte) Key {
	k := make([]byte, 0, 1+len(addr))
	k = append(k, pfxStorage)
	k = append(k, addr...)
	return keyFromBytes(k)
}

// PrefixEnd returns the exclusive upper bound for prefix iteration.
// Returns Key{} if prefix is empty or has no successor (all 0xFF).
func PrefixEnd(prefix Key) Key {
	if prefix.isZero() {
		return Key{}
	}
	b := bytes.Clone(prefix.b)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] != 0xFF {
			b[i]++
			return keyFromBytes(b[:i+1])
		}
	}
	return Key{}
}

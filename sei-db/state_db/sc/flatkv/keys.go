package flatkv

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Address is an EVM address (20 bytes).
type Address [20]byte

// CodeHash is a 32-byte contract code hash.
type CodeHash [32]byte

// Slot is a 32-byte storage slot key.
type Slot [32]byte

// Word is a fixed-size 32-byte EVM word (balance, storage value, etc.).
type Word [32]byte

// AccountKey is a type-safe account DB key.
// Zero value means "unbounded" for iterator bounds.
type AccountKey struct{ b []byte }

func (k AccountKey) isZero() bool  { return len(k.b) == 0 }
func (k AccountKey) bytes() []byte { return k.b }

// AccountKeyFor returns the account DB key for addr.
func AccountKeyFor(addr Address) AccountKey {
	b := make([]byte, 20)
	copy(b, addr[:])
	return AccountKey{b: b}
}

// CodeKey is a type-safe code DB key.
// Zero value means "unbounded" for iterator bounds.
type CodeKey struct{ b []byte }

func (k CodeKey) isZero() bool  { return len(k.b) == 0 }
func (k CodeKey) bytes() []byte { return k.b }

// CodeKeyFor returns the code DB key for codeHash.
func CodeKeyFor(codeHash CodeHash) CodeKey {
	b := make([]byte, 32)
	copy(b, codeHash[:])
	return CodeKey{b: b}
}

// StorageKey is a type-safe storage DB key (or prefix).
//
// Supported encodings:
//   - nil: unbounded
//   - addr(20): address prefix (iterates over all slots for addr)
//   - addr(20) || slot(32): full key
type StorageKey struct{ b []byte }

func (k StorageKey) isZero() bool  { return len(k.b) == 0 }
func (k StorageKey) bytes() []byte { return k.b }

// StoragePrefix returns the storage DB prefix key for addr.
func StoragePrefix(addr Address) StorageKey {
	b := make([]byte, 20)
	copy(b, addr[:])
	return StorageKey{b: b}
}

// StorageKeyFor returns the storage DB key for (addr, slot).
func StorageKeyFor(addr Address, slot Slot) StorageKey {
	b := make([]byte, 0, 20+32)
	b = append(b, addr[:]...)
	b = append(b, slot[:]...)
	return StorageKey{b: b}
}

// PrefixEnd returns the exclusive upper bound for prefix iteration.
// Returns nil if prefix is empty or has no successor (all 0xFF).
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

// AccountValue is the fixed-size account record stored in the account DB.
//
// FlatKV distinguishes "missing" from "zero" via explicit presence bits:
// a field can be present with a zero value, or absent entirely.
type AccountValue struct {
	Balance     Word
	Nonce       uint64
	CodeHash    CodeHash
	HasBalance  bool
	HasNonce    bool
	HasCodeHash bool
}

const (
	accountValueFlagHasBalance  = 1 << 0
	accountValueFlagHasNonce    = 1 << 1
	accountValueFlagHasCodeHash = 1 << 2

	// accountValueEncodedLen is the stable on-disk encoding length:
	// flags(1) || balance(32) || nonce(u64be=8) || codehash(32)
	accountValueEncodedLen = 1 + 32 + 8 + 32
)

// EncodeAccountValue encodes v into a stable, fixed-length byte slice.
func EncodeAccountValue(v AccountValue) []byte {
	b := make([]byte, 0, accountValueEncodedLen)
	var flags byte
	if v.HasBalance {
		flags |= accountValueFlagHasBalance
	}
	if v.HasNonce {
		flags |= accountValueFlagHasNonce
	}
	if v.HasCodeHash {
		flags |= accountValueFlagHasCodeHash
	}
	b = append(b, flags)
	b = append(b, v.Balance[:]...)
	var nonce [8]byte
	binary.BigEndian.PutUint64(nonce[:], v.Nonce)
	b = append(b, nonce[:]...)
	b = append(b, v.CodeHash[:]...)
	return b
}

// DecodeAccountValue decodes a fixed-length account record previously produced by EncodeAccountValue.
func DecodeAccountValue(b []byte) (AccountValue, error) {
	if len(b) != accountValueEncodedLen {
		return AccountValue{}, fmt.Errorf("invalid account value length: got %d, want %d", len(b), accountValueEncodedLen)
	}
	var v AccountValue
	flags := b[0]
	v.HasBalance = (flags & accountValueFlagHasBalance) != 0
	v.HasNonce = (flags & accountValueFlagHasNonce) != 0
	v.HasCodeHash = (flags & accountValueFlagHasCodeHash) != 0

	off := 1
	copy(v.Balance[:], b[off:off+32])
	off += 32
	v.Nonce = binary.BigEndian.Uint64(b[off : off+8])
	off += 8
	copy(v.CodeHash[:], b[off:off+32])
	return v, nil
}

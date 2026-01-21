package flatkv

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const (
	AddressLen  = 20
	CodeHashLen = 32
	SlotLen     = 32
	BalanceLen  = 32

	NonceLen = 8
)

// Address is an EVM address (20 bytes).
type Address [AddressLen]byte

// CodeHash is a contract code hash (32 bytes).
type CodeHash [CodeHashLen]byte

// Slot is a storage slot key (32 bytes).
type Slot [SlotLen]byte

// Balance is an EVM balance (32 bytes, big-endian uint256).
type Balance [BalanceLen]byte

func AddressFromBytes(b []byte) (Address, bool) {
	if len(b) != AddressLen {
		return Address{}, false
	}
	var a Address
	copy(a[:], b)
	return a, true
}

func CodeHashFromBytes(b []byte) (CodeHash, bool) {
	if len(b) != CodeHashLen {
		return CodeHash{}, false
	}
	var h CodeHash
	copy(h[:], b)
	return h, true
}

func SlotFromBytes(b []byte) (Slot, bool) {
	if len(b) != SlotLen {
		return Slot{}, false
	}
	var s Slot
	copy(s[:], b)
	return s, true
}

// AccountKey is a type-safe account DB key.
type AccountKey struct{ b []byte }

func (k AccountKey) isZero() bool  { return len(k.b) == 0 }
func (k AccountKey) bytes() []byte { return k.b }

// AccountKeyFor returns the account DB key for addr.
func AccountKeyFor(addr Address) AccountKey {
	b := make([]byte, AddressLen)
	copy(b, addr[:])
	return AccountKey{b: b}
}

// CodeKey is a type-safe code DB key.
type CodeKey struct{ b []byte }

func (k CodeKey) isZero() bool  { return len(k.b) == 0 }
func (k CodeKey) bytes() []byte { return k.b }

// CodeKeyFor returns the code DB key for codeHash.
func CodeKeyFor(codeHash CodeHash) CodeKey {
	b := make([]byte, CodeHashLen)
	copy(b, codeHash[:])
	return CodeKey{b: b}
}

// StorageKey is a type-safe storage DB key (or prefix).
// Encodes: nil (unbounded), addr (prefix), or addr||slot (full key).
type StorageKey struct{ b []byte }

func (k StorageKey) isZero() bool  { return len(k.b) == 0 }
func (k StorageKey) bytes() []byte { return k.b }

// StoragePrefix returns the storage DB prefix key for addr.
func StoragePrefix(addr Address) StorageKey {
	b := make([]byte, AddressLen)
	copy(b, addr[:])
	return StorageKey{b: b}
}

// StorageKeyFor returns the storage DB key for (addr, slot).
func StorageKeyFor(addr Address, slot Slot) StorageKey {
	b := make([]byte, 0, AddressLen+SlotLen)
	b = append(b, addr[:]...)
	b = append(b, slot[:]...)
	return StorageKey{b: b}
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

// AccountValue is the account record: balance(32) || nonce(8) || codehash(32).
type AccountValue struct {
	Balance  Balance
	Nonce    uint64
	CodeHash CodeHash
}

const (
	// accountValueEncodedLen = BalanceLen + NonceLen + CodeHashLen
	accountValueEncodedLen = BalanceLen + NonceLen + CodeHashLen
)

// EncodeAccountValue encodes v into a stable 72-byte slice.
func EncodeAccountValue(v AccountValue) []byte {
	b := make([]byte, 0, accountValueEncodedLen)
	b = append(b, v.Balance[:]...)
	var nonce [NonceLen]byte
	binary.BigEndian.PutUint64(nonce[:], v.Nonce)
	b = append(b, nonce[:]...)
	b = append(b, v.CodeHash[:]...)
	return b
}

// DecodeAccountValue decodes a fixed-length account record.
func DecodeAccountValue(b []byte) (AccountValue, error) {
	if len(b) != accountValueEncodedLen {
		return AccountValue{}, fmt.Errorf("invalid account value length: got %d, want %d", len(b), accountValueEncodedLen)
	}
	var v AccountValue
	off := 0
	copy(v.Balance[:], b[off:off+BalanceLen])
	off += BalanceLen
	v.Nonce = binary.BigEndian.Uint64(b[off : off+NonceLen])
	off += NonceLen
	copy(v.CodeHash[:], b[off:off+CodeHashLen])
	return v, nil
}

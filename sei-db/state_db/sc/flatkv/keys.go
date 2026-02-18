package flatkv

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// DBLocalMetaKey is the key for per-DB local metadata.
// It is a single-byte key (0x00), which cannot collide with any valid user key
// because all user keys have minimum length of 20 bytes (EVM address).
//
// Invariant: All user keys are >= 20 bytes (address=20, storage=52).
var DBLocalMetaKey = []byte{0x00}

// metaKeyLowerBound returns the iterator lower bound that excludes DBLocalMetaKey.
// Lexicographically: 0x00 (1 byte) < 0x00,0x00 (2 bytes) < any user key (>=20 bytes).
// This ensures metadata key is excluded while all user keys (even those starting
// with 0x00) are included.
func metaKeyLowerBound() []byte {
	return []byte{0x00, 0x00}
}

const (
	AddressLen  = 20
	CodeHashLen = 32
	SlotLen     = 32
	BalanceLen  = 32
	NonceLen    = 8

	// localMetaSize is the serialized size of LocalMeta (version = 8 bytes)
	localMetaSize = 8
)

// LocalMeta stores per-DB version tracking metadata.
// Stored inside each DB at DBLocalMetaKey (0x00).
type LocalMeta struct {
	CommittedVersion int64 // Current committed version in this DB
}

// MarshalLocalMeta encodes LocalMeta as fixed 8 bytes (big-endian).
func MarshalLocalMeta(m *LocalMeta) []byte {
	buf := make([]byte, localMetaSize)
	binary.BigEndian.PutUint64(buf, uint64(m.CommittedVersion)) //nolint:gosec // version is always non-negative
	return buf
}

// UnmarshalLocalMeta decodes LocalMeta from bytes.
func UnmarshalLocalMeta(data []byte) (*LocalMeta, error) {
	if len(data) != localMetaSize {
		return nil, fmt.Errorf("invalid LocalMeta size: got %d, want %d", len(data), localMetaSize)
	}
	return &LocalMeta{
		CommittedVersion: int64(binary.BigEndian.Uint64(data)), //nolint:gosec // version won't exceed int64 max
	}, nil
}

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

// =============================================================================
// DB Key Builders
// =============================================================================

// AccountKey returns the accountDB key for addr.
// Key format: addr(20)
func AccountKey(addr Address) []byte {
	return addr[:]
}

// CodeKey returns the codeDB key for codeHash.
// Key format: codeHash(32)
func CodeKey(codeHash CodeHash) []byte {
	return codeHash[:]
}

// StorageKey returns the storageDB key for (addr, slot).
// Key format: addr(20) || slot(32) = 52 bytes
func StorageKey(addr Address, slot Slot) []byte {
	key := make([]byte, AddressLen+SlotLen)
	copy(key[:AddressLen], addr[:])
	copy(key[AddressLen:], slot[:])
	return key
}

// StoragePrefix returns the storageDB prefix for iterating all slots of addr.
// Prefix format: addr(20)
func StoragePrefix(addr Address) []byte {
	return addr[:]
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

// AccountValue is the account record.
//
// Encoding is variable-length to save space for EOA accounts:
//   - EOA (no code):      balance(32) || nonce(8)           = 40 bytes
//   - Contract (has code): balance(32) || nonce(8) || codehash(32) = 72 bytes
//
// CodeHash == CodeHash{} (all zeros) means the account has no code (EOA).
// Note: empty code contracts have CodeHash = keccak256("") which is non-zero.
type AccountValue struct {
	Balance  Balance
	Nonce    uint64
	CodeHash CodeHash
}

const (
	// accountValueEOALen is the encoded length for EOA accounts (no code).
	accountValueEOALen = BalanceLen + NonceLen // 40 bytes

	// accountValueContractLen is the encoded length for contract accounts.
	accountValueContractLen = BalanceLen + NonceLen + CodeHashLen // 72 bytes
)

// HasCode returns true if the account has code (is a contract).
func (v AccountValue) HasCode() bool {
	return v.CodeHash != CodeHash{}
}

// Encode encodes the AccountValue to bytes.
func (v AccountValue) Encode() []byte {
	return EncodeAccountValue(v)
}

// EncodeAccountValue encodes v into a variable-length slice.
// EOA accounts (no code) are encoded as 40 bytes, contracts as 72 bytes.
func EncodeAccountValue(v AccountValue) []byte {
	if !v.HasCode() {
		// EOA: balance(32) || nonce(8)
		b := make([]byte, 0, accountValueEOALen)
		b = append(b, v.Balance[:]...)
		var nonce [NonceLen]byte
		binary.BigEndian.PutUint64(nonce[:], v.Nonce)
		b = append(b, nonce[:]...)
		return b
	}

	// Contract: balance(32) || nonce(8) || codehash(32)
	b := make([]byte, 0, accountValueContractLen)
	b = append(b, v.Balance[:]...)
	var nonce [NonceLen]byte
	binary.BigEndian.PutUint64(nonce[:], v.Nonce)
	b = append(b, nonce[:]...)
	b = append(b, v.CodeHash[:]...)
	return b
}

// DecodeAccountValue decodes a variable-length account record.
// Returns an error if the length is neither 40 (EOA) nor 72 (contract) bytes.
func DecodeAccountValue(b []byte) (AccountValue, error) {
	switch len(b) {
	case accountValueEOALen:
		// EOA: balance(32) || nonce(8)
		var v AccountValue
		copy(v.Balance[:], b[:BalanceLen])
		v.Nonce = binary.BigEndian.Uint64(b[BalanceLen:])
		// CodeHash remains zero (no code)
		return v, nil

	case accountValueContractLen:
		// Contract: balance(32) || nonce(8) || codehash(32)
		var v AccountValue
		copy(v.Balance[:], b[:BalanceLen])
		v.Nonce = binary.BigEndian.Uint64(b[BalanceLen : BalanceLen+NonceLen])
		copy(v.CodeHash[:], b[BalanceLen+NonceLen:])
		return v, nil

	default:
		return AccountValue{}, fmt.Errorf("invalid account value length: got %d, want %d (EOA) or %d (contract)",
			len(b), accountValueEOALen, accountValueContractLen)
	}
}

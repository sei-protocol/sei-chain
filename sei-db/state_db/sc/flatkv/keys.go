package flatkv

import (
	"bytes"
	"encoding/binary"
	"fmt"

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

const (
	AddressLen  = 20
	CodeHashLen = 32
	SlotLen     = 32
	BalanceLen  = 32
	NonceLen    = 8
)

// LocalMeta stores per-DB version tracking metadata.
// Version is stored at _meta/version, LtHash at _meta/hash.
type LocalMeta struct {
	CommittedVersion int64          // Current committed version in this DB
	LtHash           *lthash.LtHash // nil for old format (version-only)
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

// StorageKey returns the storageDB key for (addr, slot).
// Key format: addr(20) || slot(32) = 52 bytes
func StorageKey(addr Address, slot Slot) []byte {
	key := make([]byte, AddressLen+SlotLen)
	copy(key[:AddressLen], addr[:])
	copy(key[AddressLen:], slot[:])
	return key
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

// IsEmpty returns true when all fields are zero-valued, indicating the
// account can be physically deleted from accountDB.
func (v AccountValue) IsEmpty() bool {
	return v.Balance == (Balance{}) && v.Nonce == 0 && v.CodeHash == (CodeHash{})
}

// Encode encodes the AccountValue to bytes.
func (v AccountValue) Encode() []byte {
	return EncodeAccountValue(v)
}

// EncodeAccountValue encodes v into a variable-length slice.
// EOA accounts (no code) are encoded as 40 bytes, contracts as 72 bytes.
func EncodeAccountValue(v AccountValue) []byte {
	size := accountValueEOALen
	if v.HasCode() {
		size = accountValueContractLen
	}
	b := make([]byte, size)
	copy(b, v.Balance[:])
	binary.BigEndian.PutUint64(b[BalanceLen:], v.Nonce)
	if v.HasCode() {
		copy(b[BalanceLen+NonceLen:], v.CodeHash[:])
	}
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

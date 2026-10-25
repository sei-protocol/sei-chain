package vtype

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// AccountRow is a read-only view over a serialized account row, in either its compact or full form.
// It aliases the bytes it was parsed from rather than copying them.
type AccountRow struct {
	data []byte
}

// ParseAccountRow validates data as an account row and returns a view over it.
func ParseAccountRow(data []byte) (AccountRow, error) {
	if len(data) == 0 {
		return AccountRow{}, errors.New("data is empty")
	}
	if version := AccountDataVersion(data[accountVersionStart]); version != AccountDataVersion0 {
		return AccountRow{}, fmt.Errorf("unsupported serialization version: %d", version)
	}
	if len(data) != accountDataLength && len(data) != accountCompactLength {
		return AccountRow{}, fmt.Errorf("data length should be %d or %d, got %d",
			accountCompactLength, accountDataLength, len(data))
	}
	return AccountRow{data: data}, nil
}

// Balance returns the account's balance.
func (r AccountRow) Balance() Balance {
	return Balance(r.data[accountBalanceStart:accountNonceStart])
}

// Nonce returns the account's nonce.
func (r AccountRow) Nonce() uint64 {
	return binary.BigEndian.Uint64(r.data[accountNonceStart:accountCodeHashStart])
}

// CodeHash returns the account's code hash, or the zero hash for a compact row.
func (r AccountRow) CodeHash() CodeHash {
	if len(r.data) < accountDataLength {
		return CodeHash{}
	}
	return CodeHash(r.data[accountCodeHashStart:accountDataLength])
}

// IsDelete reports whether the row is a tombstone: every field other than the version and block
// height is zero.
func (r AccountRow) IsDelete() bool {
	for _, b := range r.data[accountBalanceStart:] {
		if b != 0 {
			return false
		}
	}
	return true
}

// StorageRow is a read-only view over a serialized storage row. It aliases the bytes it was parsed
// from rather than copying them.
type StorageRow struct {
	data []byte
}

// ParseStorageRow validates data as a storage row and returns a view over it.
func ParseStorageRow(data []byte) (StorageRow, error) {
	if len(data) == 0 {
		return StorageRow{}, errors.New("data is empty")
	}
	if version := StorageDataVersion(data[storageVersionStart]); version != StorageDataVersion0 {
		return StorageRow{}, fmt.Errorf("unsupported serialization version: %d", version)
	}
	if len(data) != storageDataLength {
		return StorageRow{}, fmt.Errorf("data length should be %d, got %d", storageDataLength, len(data))
	}
	return StorageRow{data: data}, nil
}

// Value returns the storage slot value.
func (r StorageRow) Value() [StorageValueLength]byte {
	return [StorageValueLength]byte(r.data[storageValueStart:storageDataLength])
}

// IsDelete reports whether the row is a tombstone: the value is all zeros.
func (r StorageRow) IsDelete() bool {
	for _, b := range r.data[storageValueStart:] {
		if b != 0 {
			return false
		}
	}
	return true
}

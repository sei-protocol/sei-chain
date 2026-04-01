package vtype

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type AccountDataVersion uint8

// DO NOT CHANGE VERSION VALUES!!! Adding new versions is ok, but historical versions should never be removed/changed.
const (
	// The version of the account data field when FlatKV was first launched.
	AccountDataVersion0 AccountDataVersion = 0
)

/*
Serialization schema for AccountData version 0:

Full form (81 bytes):

| Version | Block Height | Balance  | Nonce    | Code Hash |
|---------|--------------|----------|----------|-----------|
| 1 byte  | 8 bytes      | 32 bytes | 8 bytes  | 32 bytes  |

Compact form (49 bytes) — used when code hash is all zeros:

| Version | Block Height | Balance  | Nonce    |
|---------|--------------|----------|----------|
| 1 byte  | 8 bytes      | 32 bytes | 8 bytes  |

Data is stored in big-endian order. At deserialization time, the two forms
are distinguished by length. The compact form is preferred for serialization
since ~97% of accounts have no code hash.
*/

const (
	accountVersionStart     = 0
	accountBlockHeightStart = 1
	accountBalanceStart     = 9
	accountNonceStart       = 41
	accountCodeHashStart    = 49
	accountCompactLength    = accountCodeHashStart // 49
	accountDataLength       = 81
)

var _ VType = (*AccountData)(nil)

// Used for encapsulating and serializating account data in the FlatKV accounts database.
//
// This data structure is not threadsafe. Values passed into and values received from this data structure
// are not safe to modify without first copying them.
type AccountData struct {
	data []byte
}

// Create a new AccountData initialized to all 0s.
func NewAccountData() *AccountData {
	return &AccountData{
		data: make([]byte, accountDataLength),
	}
}

// Serialize the account data to a byte slice. If the code hash is all zeros,
// the compact form (49 bytes) is returned; otherwise the full form (81 bytes).
//
// The returned byte slice is not safe to modify without first copying it.
func (a *AccountData) Serialize() []byte {
	if a == nil {
		return make([]byte, accountCompactLength)
	}
	for i := accountCodeHashStart; i < accountDataLength; i++ {
		if a.data[i] != 0 {
			return a.data
		}
	}
	return a.data[:accountCompactLength]
}

// Deserialize the account data from the given byte slice. Accepts both the
// compact (49 byte) and full (81 byte) forms.
func DeserializeAccountData(data []byte) (*AccountData, error) {
	if len(data) == 0 {
		return nil, errors.New("data is empty")
	}

	version := AccountDataVersion(data[accountVersionStart])
	if version != AccountDataVersion0 {
		return nil, fmt.Errorf("unsupported serialization version: %d", version)
	}

	switch len(data) {
	case accountDataLength:
		return &AccountData{data: data}, nil
	case accountCompactLength:
		full := make([]byte, accountDataLength)
		copy(full, data)
		return &AccountData{data: full}, nil
	default:
		return nil, fmt.Errorf("data length at version %d should be %d or %d, got %d",
			version, accountCompactLength, accountDataLength, len(data))
	}
}

// Get the serialization version for this AccountData instance.
func (a *AccountData) GetSerializationVersion() AccountDataVersion {
	if a == nil {
		return AccountDataVersion0
	}
	return (AccountDataVersion)(a.data[accountVersionStart])
}

// Get the account's block height.
func (a *AccountData) GetBlockHeight() int64 {
	if a == nil {
		return 0
	}
	return int64(binary.BigEndian.Uint64(a.data[accountBlockHeightStart:accountBalanceStart])) //nolint:gosec
}

// Get the account's balance.
func (a *AccountData) GetBalance() *Balance {
	if a == nil {
		var zero Balance
		return &zero
	}
	return (*Balance)(a.data[accountBalanceStart:accountNonceStart])
}

// Get the account's nonce.
func (a *AccountData) GetNonce() uint64 {
	if a == nil {
		return 0
	}
	return binary.BigEndian.Uint64(a.data[accountNonceStart:accountCodeHashStart])
}

// Get the account's code hash.
func (a *AccountData) GetCodeHash() *CodeHash {
	if a == nil {
		var zero CodeHash
		return &zero
	}
	return (*CodeHash)(a.data[accountCodeHashStart:accountDataLength])
}

// Check if this account data signifies a deletion operation. A deletion operation is automatically
// performed when all account data fields are 0 (with the exception of the serialization version and block height).
func (a *AccountData) IsDelete() bool {
	if a == nil {
		return true
	}
	for i := accountBalanceStart; i < accountDataLength; i++ {
		if a.data[i] != 0 {
			return false
		}
	}
	return true
}

// Copy returns a deep copy of this AccountData. The copy has its own backing byte slice.
func (a *AccountData) Copy() *AccountData {
	if a == nil {
		return NewAccountData()
	}
	cp := make([]byte, len(a.data))
	copy(cp, a.data)
	return &AccountData{data: cp}
}

// Set the account's block height when this account was last modified/touched. Returns self.
func (a *AccountData) SetBlockHeight(blockHeight int64) *AccountData {
	if a == nil {
		a = NewAccountData()
	}
	binary.BigEndian.PutUint64(a.data[accountBlockHeightStart:accountBalanceStart], uint64(blockHeight)) //nolint:gosec
	return a
}

// Set the account's balance. Returns self (or a new AccountData if nil).
func (a *AccountData) SetBalance(balance *Balance) *AccountData {
	if a == nil {
		a = NewAccountData()
	}
	if balance == nil {
		var zero Balance
		balance = &zero
	}
	copy(a.data[accountBalanceStart:accountNonceStart], balance[:])
	return a
}

// Set the account's nonce. Returns self (or a new AccountData if nil).
func (a *AccountData) SetNonce(nonce uint64) *AccountData {
	if a == nil {
		a = NewAccountData()
	}
	binary.BigEndian.PutUint64(a.data[accountNonceStart:accountCodeHashStart], nonce)
	return a
}

// Set the account's code hash. Returns self (or a new AccountData if nil).
func (a *AccountData) SetCodeHash(codeHash *CodeHash) *AccountData {
	if a == nil {
		a = NewAccountData()
	}
	if codeHash == nil {
		var zero CodeHash
		codeHash = &zero
	}
	copy(a.data[accountCodeHashStart:accountDataLength], codeHash[:])
	return a
}

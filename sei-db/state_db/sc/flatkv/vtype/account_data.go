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
are distinguished by length. The compact form is always used when the code
hash is all zeros, and the full form only when it is not, so every account
has exactly one encoding.
*/

const (
	accountVersionStart     = 0
	accountBlockHeightStart = accountVersionStart + VersionLength
	accountBalanceStart     = accountBlockHeightStart + BlockHeightLength
	accountNonceStart       = accountBalanceStart + BalanceLength
	accountCodeHashStart    = accountNonceStart + NonceLength

	accountCompactLength = VersionLength + BlockHeightLength + BalanceLength + NonceLength
	accountDataLength    = VersionLength + BlockHeightLength + BalanceLength + NonceLength + CodeHashLength
)

// AccountData is an account row in the FlatKV accounts database. The zero value is an account with every
// field zero.
type AccountData struct {
	// The block height at which this account was last modified.
	blockHeight uint64

	// The account's balance.
	balance Balance

	// The account's nonce.
	nonce uint64

	// The hash of the account's contract code, or all zeros when it has none.
	codeHash CodeHash
}

// NewAccountData returns a new AccountData with every field zero.
func NewAccountData() *AccountData {
	return &AccountData{}
}

// AppendAccountData appends the serialized form of account to dst and returns the extended slice. The
// compact form (49 bytes) is used when the code hash is all zeros, and the full form (81 bytes) otherwise.
func AppendAccountData(dst []byte, account AccountData) []byte {
	dst = append(dst, byte(AccountDataVersion0))
	dst = binary.BigEndian.AppendUint64(dst, account.blockHeight)
	dst = append(dst, account.balance[:]...)
	dst = binary.BigEndian.AppendUint64(dst, account.nonce)
	if account.codeHash != (CodeHash{}) {
		dst = append(dst, account.codeHash[:]...)
	}
	return dst
}

// Serialize returns the serialized form of the account in a new slice (see AppendAccountData).
func (a AccountData) Serialize() []byte {
	return AppendAccountData(make([]byte, 0, accountDataLength), a)
}

// DeserializeAccountData parses an account from its serialized form. Accepts both the compact (49 byte)
// and full (81 byte) forms, and rejects a full form whose code hash is all zeros, which has no encoding
// that AppendAccountData would reproduce.
func DeserializeAccountData(data []byte) (AccountData, error) {
	if len(data) == 0 {
		return AccountData{}, errors.New("data is empty")
	}

	version := AccountDataVersion(data[accountVersionStart])
	if version != AccountDataVersion0 {
		return AccountData{}, fmt.Errorf("unsupported serialization version: %d", version)
	}
	if len(data) != accountDataLength && len(data) != accountCompactLength {
		return AccountData{}, fmt.Errorf("data length at version %d should be %d or %d, got %d",
			version, accountCompactLength, accountDataLength, len(data))
	}

	account := AccountData{
		blockHeight: binary.BigEndian.Uint64(data[accountBlockHeightStart:accountBalanceStart]),
		balance:     Balance(data[accountBalanceStart:accountNonceStart]),
		nonce:       binary.BigEndian.Uint64(data[accountNonceStart:accountCodeHashStart]),
	}
	if len(data) == accountDataLength {
		account.codeHash = CodeHash(data[accountCodeHashStart:accountDataLength])
		if account.codeHash == (CodeHash{}) {
			return AccountData{}, errors.New("full-form account row has an all-zero code hash")
		}
	}
	return account, nil
}

// GetBlockHeight returns the block height at which the account was last modified.
func (a AccountData) GetBlockHeight() uint64 {
	return a.blockHeight
}

// GetBalance returns the account's balance.
func (a AccountData) GetBalance() Balance {
	return a.balance
}

// GetNonce returns the account's nonce.
func (a AccountData) GetNonce() uint64 {
	return a.nonce
}

// GetCodeHash returns the account's code hash, or all zeros when it has no code.
func (a AccountData) GetCodeHash() CodeHash {
	return a.codeHash
}

// IsDelete reports whether this account is empty: every field other than the block height is zero. The
// store deletes an account that becomes empty.
func (a AccountData) IsDelete() bool {
	return a.nonce == 0 && a.balance == (Balance{}) && a.codeHash == (CodeHash{})
}

// SetBlockHeight sets the block height at which the account was last modified. Returns the receiver.
func (a *AccountData) SetBlockHeight(blockHeight uint64) *AccountData {
	a.blockHeight = blockHeight
	return a
}

// SetBalance sets the account's balance. A nil balance is all zeros. Returns the receiver.
func (a *AccountData) SetBalance(balance *Balance) *AccountData {
	if balance == nil {
		a.balance = Balance{}
	} else {
		a.balance = *balance
	}
	return a
}

// SetNonce sets the account's nonce. Returns the receiver.
func (a *AccountData) SetNonce(nonce uint64) *AccountData {
	a.nonce = nonce
	return a
}

// SetCodeHash sets the account's code hash. A nil code hash is all zeros. Returns the receiver.
func (a *AccountData) SetCodeHash(codeHash *CodeHash) *AccountData {
	if codeHash == nil {
		a.codeHash = CodeHash{}
	} else {
		a.codeHash = *codeHash
	}
	return a
}

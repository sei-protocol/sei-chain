// Package stateview serves Giga's read API from a RawStateView, decoding the key-values the state stores hold.
package stateview

import (
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

var _ gigatypes.StateView = (*stateView)(nil)

// stateView decodes the key-values of the RawStateView it embeds. Closing it closes that view.
type stateView struct {
	gigatypes.RawStateView
}

// New returns a StateView that decodes the key-values raw holds. Closing it closes raw.
func New(raw gigatypes.RawStateView) gigatypes.StateView {
	return &stateView{RawStateView: raw}
}

func (v *stateView) Get(module string, key []byte) ([]byte, bool) {
	if module != keys.EVMStoreKey {
		return v.miscValue(module, key)
	}

	kind, keyBytes := keys.ParseEVMKey(key)
	switch kind {
	case keys.EVMKeyEmpty:
		return nil, false

	case keys.EVMKeyNonce, keys.EVMKeyCodeHash, keys.EVMKeyBalance:
		account := v.accountData(keyBytes)
		if account == nil {
			return nil, false
		}
		return account.FieldValue(kind)

	case keys.EVMKeyStorage:
		storage := v.storageData(keyBytes)
		if storage == nil {
			return nil, false
		}
		value := storage.GetValue()
		return value[:], true

	case keys.EVMKeyCode:
		code := v.codeData(keyBytes)
		if code == nil {
			return nil, false
		}
		return code.GetBytecode(), true

	case keys.EVMKeyMisc:
		return v.miscValue(keys.EVMStoreKey, keyBytes)

	default:
		panic(fmt.Sprintf("stateview: Get unsupported key type: %v", kind))
	}
}

// AccountExists reports whether addr has an account in this block.
func (v *stateView) AccountExists(addr gigatypes.Address) bool {
	_, ok := v.storedAccount(addr)
	return ok
}

// GetNonce returns addr's account nonce, or 0 when the account does not exist.
func (v *stateView) GetNonce(addr gigatypes.Address) uint64 {
	account, ok := v.storedAccount(addr)
	if !ok {
		return 0
	}
	return account.Nonce()
}

// GetBalance returns addr's balance as a 256-bit big-endian value, or the zero value when addr holds
// no balance.
func (v *stateView) GetBalance(addr gigatypes.Address) gigatypes.Hash {
	account, ok := v.storedAccount(addr)
	if !ok {
		return gigatypes.Hash{}
	}
	return gigatypes.Hash(account.Balance())
}

// GetCodeHash returns the hash of addr's contract code, gigatypes.EmptyCodeHash when the account exists
// and holds no code, or the zero hash when it does not exist.
func (v *stateView) GetCodeHash(addr gigatypes.Address) gigatypes.Hash {
	account, ok := v.storedAccount(addr)
	if !ok {
		return gigatypes.Hash{}
	}
	codeHash := gigatypes.Hash(account.CodeHash())
	if codeHash == (gigatypes.Hash{}) {
		// A stored account with a zero code hash exists and has no code.
		return gigatypes.EmptyCodeHash
	}
	return codeHash
}

// ReadAccount returns addr's balance, nonce and code hash from one read of the stored account.
func (v *stateView) ReadAccount(addr gigatypes.Address) (gigatypes.Account, bool) {
	account, ok := v.storedAccount(addr)
	if !ok {
		return gigatypes.Account{}, false
	}
	codeHash := gigatypes.Hash(account.CodeHash())
	if codeHash == (gigatypes.Hash{}) {
		// A stored account with a zero code hash exists and has no code.
		codeHash = gigatypes.EmptyCodeHash
	}
	return gigatypes.Account{
		Balance:  gigatypes.Hash(account.Balance()),
		Nonce:    account.Nonce(),
		CodeHash: codeHash,
	}, true
}

// GetStorage returns the value at key in addr's storage, or the zero hash when the slot is unset.
func (v *stateView) GetStorage(addr gigatypes.Address, key gigatypes.Hash) gigatypes.Hash {
	raw, found := v.readEVM(keys.EVMKeyStorage, addr[:], key[:])
	if !found {
		return gigatypes.Hash{}
	}
	storage, err := vtype.ParseStorageRow(raw)
	if err != nil {
		panic(fmt.Sprintf("stateview: parse storage %x/%x at height %d: %v", addr, key, v.GetBlockHeight(), err))
	}
	if storage.IsDelete() {
		return gigatypes.Hash{}
	}
	return gigatypes.Hash(storage.Value())
}

// GetCode returns addr's contract code, or nil when it has none. The slice aliases the stored value
// and is valid until the view is closed.
func (v *stateView) GetCode(addr gigatypes.Address) []byte {
	code := v.codeData(addr[:])
	if code == nil {
		return nil
	}
	return code.GetBytecode()
}

// GetCodeSize returns the length of addr's contract code in bytes, or 0 when it has none.
func (v *stateView) GetCodeSize(addr gigatypes.Address) int {
	return len(v.GetCode(addr))
}

// physKeyBufLen holds the longest EVM physical key: "evm/" + kind byte + address + slot.
const physKeyBufLen = len(keys.EVMStoreKey) + 2 + ktype.AddressLen + ktype.SlotLen

// physKeyBufs holds scratch buffers for building physical keys that live only for one read.
var physKeyBufs = sync.Pool{New: func() any { return new([physKeyBufLen]byte) }}

// storedAccount returns addr's stored account, or false when no account exists in this block. It aliases
// the stored value and is valid until the view is closed.
func (v *stateView) storedAccount(addr gigatypes.Address) (vtype.AccountRow, bool) {
	raw, found := v.readEVM(ktype.EVMKeyAccount, addr[:])
	if !found {
		return vtype.AccountRow{}, false
	}
	account, err := vtype.ParseAccountRow(raw)
	if err != nil {
		panic(fmt.Sprintf("stateview: parse account %x at height %d: %v", addr, v.GetBlockHeight(), err))
	}
	if account.IsDelete() {
		return vtype.AccountRow{}, false
	}
	return account, true
}

// accountData returns the account for the 20-byte address in keyBytes, or nil when no account exists in
// this block.
func (v *stateView) accountData(keyBytes []byte) *vtype.AccountData {
	raw, found := v.readEVM(ktype.EVMKeyAccount, keyBytes)
	account, err := decode(raw, found, vtype.DeserializeAccountData)
	if err != nil {
		panic(fmt.Sprintf("stateview: parse account %x at height %d: %v", keyBytes, v.GetBlockHeight(), err))
	}
	if account == nil || account.IsDelete() {
		return nil
	}
	return account
}

// storageData returns the storage value for the addr||slot in keyBytes, or nil when the slot is unset.
func (v *stateView) storageData(keyBytes []byte) *vtype.StorageData {
	raw, found := v.readEVM(keys.EVMKeyStorage, keyBytes)
	storage, err := decode(raw, found, vtype.DeserializeStorageData)
	if err != nil {
		panic(fmt.Sprintf("stateview: parse storage %x at height %d: %v", keyBytes, v.GetBlockHeight(), err))
	}
	if storage == nil || storage.IsDelete() {
		return nil
	}
	return storage
}

// codeData returns the code for the 20-byte address in keyBytes, or nil when it has none.
func (v *stateView) codeData(keyBytes []byte) *vtype.CodeData {
	raw, found := v.readEVM(keys.EVMKeyCode, keyBytes)
	code, err := decode(raw, found, vtype.DeserializeCodeData)
	if err != nil {
		panic(fmt.Sprintf("stateview: parse code for %x at height %d: %v", keyBytes, v.GetBlockHeight(), err))
	}
	if code == nil || code.IsDelete() {
		return nil
	}
	return code
}

// miscValue returns the value stored under keyBytes in the named module, and whether it was found.
func (v *stateView) miscValue(module string, keyBytes []byte) ([]byte, bool) {
	raw, found := v.GetRaw(ktype.BuildPhysicalKey(module, keyBytes))
	misc, err := decode(raw, found, vtype.DeserializeMiscData)
	if err != nil {
		panic(fmt.Sprintf("stateview: parse misc %s/%x at height %d: %v", module, keyBytes, v.GetBlockHeight(), err))
	}
	if misc == nil || misc.IsDelete() {
		return nil, false
	}
	value := misc.GetValue()
	return value, value != nil
}

// readEVM returns the bytes stored under the EVM physical key for kind and key parts.
func (v *stateView) readEVM(kind keys.EVMKeyKind, keyParts ...[]byte) ([]byte, bool) {
	buf := physKeyBufs.Get().(*[physKeyBufLen]byte)
	physKey := ktype.WritePhysicalEVMKeyToBuffer(buf[:], kind, keyParts[0])
	for _, keyPart := range keyParts[1:] {
		physKey = append(physKey, keyPart...)
	}
	value, found := v.GetRaw(physKey)
	// Not deferred: after a GetRaw panic, a read worker may still hold physKey, so buf must not be reused.
	physKeyBufs.Put(buf)
	return value, found
}

// decode parses raw with parse when found, and returns the zero value when not.
func decode[T vtype.VType](raw []byte, found bool, parse func([]byte) (T, error)) (T, error) {
	var zero T
	if !found {
		return zero, nil
	}
	return parse(raw)
}

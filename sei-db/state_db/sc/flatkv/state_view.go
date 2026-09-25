package flatkv

import (
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

var _ gigatypes.StateView = (*flatKVStateView)(nil)

// flatKVStateView serves the Giga read API from one committed block.
type flatKVStateView struct {
	// The block being read. Close() releases the reservation it carries.
	blockView *sview.StoreView

	// Guards the release, so a second Close does not release a reservation this view no longer owns.
	closeOnce sync.Once

	// Closed by Close.
	closed utils.CloseMarker[flatKVStateView]
}

// GetBlockHeight returns the block height of this view.
func (v *flatKVStateView) GetBlockHeight() int64 {
	return v.blockView.BlockHeight()
}

// Close releases the reservation this view holds. The view must not be read afterwards.
// Idempotent.
func (v *flatKVStateView) Close() {
	v.closeOnce.Do(func() {
		v.closed.Close(v)
		if err := v.blockView.Release(); err != nil {
			panic(fmt.Sprintf("flatkv: close state view at height %d: %v", v.blockView.BlockHeight(), err))
		}
	})
}

func (v *flatKVStateView) Get(module string, key []byte) ([]byte, bool) {
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
		return accountFieldValue(kind, account)

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
		panic(fmt.Sprintf("flatkv: Get unsupported key type: %v", kind))
	}
}

// AccountExists reports whether addr has an account in this block.
func (v *flatKVStateView) AccountExists(addr gigatypes.Address) bool {
	_, ok := v.accountRow(addr)
	return ok
}

// GetNonce returns addr's account nonce, or 0 when the account does not exist.
func (v *flatKVStateView) GetNonce(addr gigatypes.Address) uint64 {
	account, ok := v.accountRow(addr)
	if !ok {
		return 0
	}
	return account.Nonce()
}

// GetBalance returns addr's balance as a 256-bit big-endian value, or the zero value when addr holds
// no balance.
func (v *flatKVStateView) GetBalance(addr gigatypes.Address) gigatypes.Hash {
	account, ok := v.accountRow(addr)
	if !ok {
		return gigatypes.Hash{}
	}
	return gigatypes.Hash(account.Balance())
}

// GetCodeHash returns the hash of addr's contract code, gigatypes.EmptyCodeHash when the account exists
// and holds no code, or the zero hash when it does not exist.
func (v *flatKVStateView) GetCodeHash(addr gigatypes.Address) gigatypes.Hash {
	account, ok := v.accountRow(addr)
	if !ok {
		return gigatypes.Hash{}
	}
	codeHash := gigatypes.Hash(account.CodeHash())
	if codeHash == (gigatypes.Hash{}) {
		// A row only exists while some field is non-zero (see AccountData.IsDelete), and the code hash
		// is not that field here, so this account has a nonce or a balance and no code — the case EVM
		// semantics answer with the empty-code hash rather than with zero.
		return gigatypes.EmptyCodeHash
	}
	return codeHash
}

// GetStorage returns the value at key in addr's storage, or the zero hash when the slot is unset.
func (v *flatKVStateView) GetStorage(addr gigatypes.Address, key gigatypes.Hash) gigatypes.Hash {
	raw, found := v.readEVMRow(v.blockView.StorageView(), keys.EVMKeyStorage, addr[:], key[:])
	if !found {
		return gigatypes.Hash{}
	}
	storage, err := vtype.ParseStorageRow(raw)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse storage %x/%x at height %d: %v",
			addr, key, v.blockView.BlockHeight(), err))
	}
	if storage.IsDelete() {
		return gigatypes.Hash{}
	}
	return gigatypes.Hash(storage.Value())
}

// GetCode returns addr's contract code, or nil when it has none. The slice aliases the store's row
// and is valid until the view is closed.
func (v *flatKVStateView) GetCode(addr gigatypes.Address) []byte {
	code := v.codeData(addr[:])
	if code == nil {
		return nil
	}
	return code.GetBytecode()
}

// GetCodeSize returns the length of addr's contract code in bytes, or 0 when it has none.
func (v *flatKVStateView) GetCodeSize(addr gigatypes.Address) int {
	return len(v.GetCode(addr))
}

// physKeyBufLen holds the longest EVM physical key: "evm/" + kind byte + address + slot.
const physKeyBufLen = len(keys.EVMStoreKey) + 2 + ktype.AddressLen + ktype.SlotLen

// physKeyBufs holds scratch buffers for building physical keys that live only for one read.
var physKeyBufs = sync.Pool{New: func() any { return new([physKeyBufLen]byte) }}

// accountRow returns addr's account row, or false when no account exists in this block. The row
// aliases store memory and is valid until the view is closed.
func (v *flatKVStateView) accountRow(addr gigatypes.Address) (vtype.AccountRow, bool) {
	raw, found := v.readEVMRow(v.blockView.AccountView(), ktype.EVMKeyAccount, addr[:])
	if !found {
		return vtype.AccountRow{}, false
	}
	account, err := vtype.ParseAccountRow(raw)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse account %x at height %d: %v", addr, v.blockView.BlockHeight(), err))
	}
	if account.IsDelete() {
		return vtype.AccountRow{}, false
	}
	return account, true
}

// accountData returns the account row for the 20-byte address in keyBytes, or nil when no account
// exists in this block.
func (v *flatKVStateView) accountData(keyBytes []byte) *vtype.AccountData {
	raw, found := v.readEVMRow(v.blockView.AccountView(), ktype.EVMKeyAccount, keyBytes)
	account, err := parseRow(raw, found, vtype.DeserializeAccountData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse account %x at height %d: %v",
			keyBytes, v.blockView.BlockHeight(), err))
	}
	if account == nil || account.IsDelete() {
		return nil
	}
	return account
}

// storageData returns the storage row for the addr||slot in keyBytes, or nil when the slot is unset.
func (v *flatKVStateView) storageData(keyBytes []byte) *vtype.StorageData {
	raw, found := v.readEVMRow(v.blockView.StorageView(), keys.EVMKeyStorage, keyBytes)
	storage, err := parseRow(raw, found, vtype.DeserializeStorageData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse storage %x at height %d: %v",
			keyBytes, v.blockView.BlockHeight(), err))
	}
	if storage == nil || storage.IsDelete() {
		return nil
	}
	return storage
}

// codeData returns the code row for the 20-byte address in keyBytes, or nil when it has no code.
func (v *flatKVStateView) codeData(keyBytes []byte) *vtype.CodeData {
	raw, found := v.readEVMRow(v.blockView.CodeView(), keys.EVMKeyCode, keyBytes)
	code, err := parseRow(raw, found, vtype.DeserializeCodeData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse code for %x at height %d: %v",
			keyBytes, v.blockView.BlockHeight(), err))
	}
	if code == nil || code.IsDelete() {
		return nil
	}
	return code
}

// miscValue returns the value stored under keyBytes in the named module, and whether it was found.
func (v *flatKVStateView) miscValue(module string, keyBytes []byte) ([]byte, bool) {
	raw, found := v.readRow(v.blockView.MiscView(), ktype.ModulePhysicalKey(module, keyBytes))
	misc, err := parseRow(raw, found, vtype.DeserializeMiscData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse misc %s/%x at height %d: %v",
			module, keyBytes, v.blockView.BlockHeight(), err))
	}
	if misc == nil || misc.IsDelete() {
		return nil, false
	}
	value := misc.GetValue()
	return value, value != nil
}

// readEVMRow returns the bytes stored under an EVM physical key for kind and key parts.
func (v *flatKVStateView) readEVMRow(dbView view.View, kind keys.EVMKeyKind, keyParts ...[]byte) ([]byte, bool) {
	buf := physKeyBufs.Get().(*[physKeyBufLen]byte)
	physKey := ktype.AppendEVMPhysicalKey(buf[:0], kind, keyParts[0])
	for _, keyPart := range keyParts[1:] {
		physKey = append(physKey, keyPart...)
	}
	value, found := v.readRow(dbView, physKey)
	// Not deferred: readRow panics when the manager shuts down while a read worker may still hold
	// physKey, and a buffer that may still be read must not go back to the pool.
	physKeyBufs.Put(buf)
	return value, found
}

// readRow returns the bytes stored under physKey, without deserializing them.
func (v *flatKVStateView) readRow(dbView view.View, physKey []byte) ([]byte, bool) {
	value, found, err := dbView.Get(physKey, true)
	if err != nil {
		panic(fmt.Sprintf("flatkv: %s read of key %x at height %d: %v",
			dbView.Name(), physKey, v.blockView.BlockHeight(), err))
	}
	return value, found
}

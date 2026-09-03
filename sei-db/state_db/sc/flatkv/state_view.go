package flatkv

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

var _ giga.StateView = (*flatKVStateView)(nil)

// flatKVStateView serves the Giga read API from one committed block.
type flatKVStateView struct {
	// The block being read. Close() hands back the reservation it carries.
	blockView *storeView

	// Guards the hand-back, so a second Close does not release a reservation this view no longer owns.
	closeOnce sync.Once
}

// GetBlockHeight returns the block height of this view.
func (v *flatKVStateView) GetBlockHeight() int64 {
	return v.blockView.blockHeight
}

// Close hands back the reservation this view holds. The view must not be read afterwards.
// Idempotent.
func (v *flatKVStateView) Close() {
	v.closeOnce.Do(func() {
		if err := v.blockView.release(); err != nil {
			panic(fmt.Sprintf("flatkv: close state view at height %d: %v", v.blockView.blockHeight, err))
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

	case keys.EVMKeyNonce, keys.EVMKeyCodeHash:
		account := v.accountData(keyBytes)
		if account == nil {
			return nil, false
		}
		if kind == keys.EVMKeyNonce {
			nonceBytes := make([]byte, vtype.NonceLen)
			binary.BigEndian.PutUint64(nonceBytes, account.GetNonce())
			return nonceBytes, true
		}
		codeHash := account.GetCodeHash()
		var zeroCodeHash vtype.CodeHash
		if *codeHash == zeroCodeHash {
			return nil, false
		}
		return codeHash[:], true

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
func (v *flatKVStateView) AccountExists(addr giga.Address) bool {
	return v.accountData(addr[:]) != nil
}

// GetNonce returns addr's account nonce, and whether addr has an account.
func (v *flatKVStateView) GetNonce(addr giga.Address) (uint64, bool) {
	account := v.accountData(addr[:])
	if account == nil {
		return 0, false
	}
	return account.GetNonce(), true
}

// GetBalance panics. FlatKV stores no balances, so there is no entry to report as present or absent,
// and either answer would misdescribe an account that holds one.
func (v *flatKVStateView) GetBalance(giga.Address) (giga.Hash, bool) {
	panic("flatkv: GetBalance is unimplemented; FlatKV does not store balances")
}

// GetCodeHash returns the hash of addr's contract code, and whether a code hash is stored for addr.
// An account that exists and holds no code stores no code hash.
func (v *flatKVStateView) GetCodeHash(addr giga.Address) (giga.Hash, bool) {
	account := v.accountData(addr[:])
	if account == nil {
		return giga.Hash{}, false
	}
	codeHash := giga.Hash(*account.GetCodeHash())
	if codeHash == (giga.Hash{}) {
		// A row only exists while some field is non-zero (see AccountData.IsDelete), and the code hash
		// is not that field here, so this account has a nonce or a balance and no code at all.
		return giga.Hash{}, false
	}
	return codeHash, true
}

// GetStorage returns the value at key in addr's storage, and whether that slot is set.
func (v *flatKVStateView) GetStorage(addr giga.Address, key giga.Hash) (giga.Hash, bool) {
	storage := v.storageData(ktype.StorageKey(ktype.Address(addr), ktype.Slot(key)))
	if storage == nil {
		return giga.Hash{}, false
	}
	return giga.Hash(*storage.GetValue()), true
}

// GetCode returns addr's contract code, and whether addr has code.
func (v *flatKVStateView) GetCode(addr giga.Address) ([]byte, bool) {
	code := v.codeData(addr[:])
	if code == nil {
		return nil, false
	}
	return code.GetBytecode(), true
}

// GetCodeSize returns the length of addr's contract code in bytes, and whether addr has code.
func (v *flatKVStateView) GetCodeSize(addr giga.Address) (int, bool) {
	code, ok := v.GetCode(addr)
	return len(code), ok
}

// accountData returns the account row for the 20-byte address in keyBytes, or nil when no account
// exists in this block.
func (v *flatKVStateView) accountData(keyBytes []byte) *vtype.AccountData {
	raw, found := v.readRow(v.blockView.accountStoreView, ktype.EVMPhysicalKey(ktype.EVMKeyAccount, keyBytes))
	account, err := parseRow(raw, found, vtype.DeserializeAccountData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse account %x at height %d: %v", keyBytes, v.blockView.blockHeight, err))
	}
	if account == nil || account.IsDelete() {
		return nil
	}
	return account
}

// storageData returns the storage row for the addr||slot in keyBytes, or nil when the slot is unset.
func (v *flatKVStateView) storageData(keyBytes []byte) *vtype.StorageData {
	raw, found := v.readRow(v.blockView.storageStoreView, ktype.EVMPhysicalKey(keys.EVMKeyStorage, keyBytes))
	storage, err := parseRow(raw, found, vtype.DeserializeStorageData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse storage %x at height %d: %v", keyBytes, v.blockView.blockHeight, err))
	}
	if storage == nil || storage.IsDelete() {
		return nil
	}
	return storage
}

// codeData returns the code row for the 20-byte address in keyBytes, or nil when it has no code.
func (v *flatKVStateView) codeData(keyBytes []byte) *vtype.CodeData {
	raw, found := v.readRow(v.blockView.codeStoreView, ktype.EVMPhysicalKey(keys.EVMKeyCode, keyBytes))
	code, err := parseRow(raw, found, vtype.DeserializeCodeData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse code for %x at height %d: %v", keyBytes, v.blockView.blockHeight, err))
	}
	if code == nil || code.IsDelete() {
		return nil
	}
	return code
}

// miscValue returns the value stored under keyBytes in the named module, and whether it was found.
func (v *flatKVStateView) miscValue(module string, keyBytes []byte) ([]byte, bool) {
	raw, found := v.readRow(v.blockView.miscStoreView, ktype.ModulePhysicalKey(module, keyBytes))
	misc, err := parseRow(raw, found, vtype.DeserializeMiscData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse misc %s/%x at height %d: %v",
			module, keyBytes, v.blockView.blockHeight, err))
	}
	if misc == nil || misc.IsDelete() {
		return nil, false
	}
	value := misc.GetValue()
	return value, value != nil
}

// readRow returns the bytes stored under physKey, without deserializing them.
func (v *flatKVStateView) readRow(dbView view.View, physKey []byte) ([]byte, bool) {
	value, found, err := dbView.Get(physKey, true)
	if err != nil {
		panic(fmt.Sprintf("flatkv: %s read of key %x at height %d: %v",
			dbView.Name(), physKey, v.blockView.blockHeight, err))
	}
	return value, found
}

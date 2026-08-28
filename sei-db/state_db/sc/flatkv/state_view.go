package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

var _ giga.StateView = (*flatKVStateView)(nil)

// flatKVStateView serves the Giga read API from one committed block.
//
// None of its methods return an error, so a read that fails panics. The alternative is answering a
// caller that has no way to tell a real value from a failed read.
type flatKVStateView struct {
	// The block being read. Close() hands back the reservation it carries.
	blockView *storeView
}

// GetBlockHeight returns the block height of this view.
func (v *flatKVStateView) GetBlockHeight() int64 {
	return v.blockView.blockHeight
}

// Close hands back the reservation this view holds. The view must not be read afterwards.
func (v *flatKVStateView) Close() {
	if err := v.blockView.release(); err != nil {
		panic(fmt.Sprintf("flatkv: close state view at height %d: %v", v.blockView.blockHeight, err))
	}
}

// Get returns the row stored under key in the named module, exactly as it sits in the database.
//
// It reads whole rows and interprets none of them: an account key answers with the serialized account,
// carrying nonce, balance and code hash together, not with any one of those fields. Use EVMStateView's
// accessors to read a field.
//
// A code-hash key names a field of the account row rather than a row of its own, since FlatKV stores
// the account as one record (see ktype.EVMKeyAccount), so it is refused.
func (v *flatKVStateView) Get(module string, key []byte) ([]byte, bool) {
	if module != keys.EVMStoreKey {
		return v.readRow(v.blockView.miscStoreView, ktype.ModulePhysicalKey(module, key))
	}

	kind, keyBytes := keys.ParseEVMKey(key)
	switch kind {
	case keys.EVMKeyEmpty:
		return nil, false
	case keys.EVMKeyNonce:
		// The canonical account-row key; ktype.EVMKeyAccount is this same prefix.
		return v.readRow(v.blockView.accountStoreView, ktype.EVMPhysicalKey(ktype.EVMKeyAccount, keyBytes))
	case keys.EVMKeyStorage:
		return v.readRow(v.blockView.storageStoreView, ktype.EVMPhysicalKey(keys.EVMKeyStorage, keyBytes))
	case keys.EVMKeyCode:
		return v.readRow(v.blockView.codeStoreView, ktype.EVMPhysicalKey(keys.EVMKeyCode, keyBytes))
	case keys.EVMKeyMisc:
		return v.readRow(v.blockView.miscStoreView, ktype.ModulePhysicalKey(keys.EVMStoreKey, keyBytes))
	case keys.EVMKeyCodeHash:
		panic(fmt.Sprintf("flatkv: Get key %x addresses the code hash field; read the account row "+
			"or call GetCodeHash", key))
	default:
		panic(fmt.Sprintf("flatkv: Get unsupported key type: %v", kind))
	}
}

// AccountExists reports whether addr has an account in this block.
func (v *flatKVStateView) AccountExists(addr giga.Address) bool {
	return v.accountData(addr) != nil
}

// GetNonce returns addr's account nonce, or 0 when the account does not exist.
func (v *flatKVStateView) GetNonce(addr giga.Address) uint64 {
	account := v.accountData(addr)
	if account == nil {
		return 0
	}
	return account.GetNonce()
}

// GetBalance returns addr's balance as a 256-bit big-endian value, or zero when the account does not
// exist.
func (v *flatKVStateView) GetBalance(addr giga.Address) giga.Hash {
	account := v.accountData(addr)
	if account == nil {
		return giga.Hash{}
	}
	return giga.Hash(*account.GetBalance())
}

// GetCodeHash returns the code hash stored for addr, or the zero hash when none is stored.
func (v *flatKVStateView) GetCodeHash(addr giga.Address) giga.Hash {
	account := v.accountData(addr)
	if account == nil {
		return giga.Hash{}
	}
	return giga.Hash(*account.GetCodeHash())
}

// GetStorage returns the value at key in addr's storage, or the zero hash when the slot is unset.
func (v *flatKVStateView) GetStorage(addr giga.Address, key giga.Hash) giga.Hash {
	physKey := ktype.EVMPhysicalKey(keys.EVMKeyStorage, ktype.StorageKey(ktype.Address(addr), ktype.Slot(key)))
	raw, found := v.readRow(v.blockView.storageStoreView, physKey)
	storage, err := parseRow(raw, found, vtype.DeserializeStorageData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse storage %x/%x at height %d: %v",
			addr, key, v.blockView.blockHeight, err))
	}
	if storage == nil || storage.IsDelete() {
		return giga.Hash{}
	}
	return giga.Hash(*storage.GetValue())
}

// GetCode returns addr's contract code, or nil when it has none.
func (v *flatKVStateView) GetCode(addr giga.Address) []byte {
	raw, found := v.readRow(v.blockView.codeStoreView, ktype.EVMPhysicalKey(keys.EVMKeyCode, addr[:]))
	code, err := parseRow(raw, found, vtype.DeserializeCodeData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse code for %x at height %d: %v", addr, v.blockView.blockHeight, err))
	}
	if code == nil || code.IsDelete() {
		return nil
	}
	return code.GetBytecode()
}

// GetCodeSize returns the length of addr's contract code in bytes, or 0 when it has none.
func (v *flatKVStateView) GetCodeSize(addr giga.Address) int {
	return len(v.GetCode(addr))
}

// accountData returns addr's account row, or nil when the account does not exist in this block.
func (v *flatKVStateView) accountData(addr giga.Address) *vtype.AccountData {
	raw, found := v.readRow(v.blockView.accountStoreView, ktype.EVMPhysicalKey(ktype.EVMKeyAccount, addr[:]))
	account, err := parseRow(raw, found, vtype.DeserializeAccountData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse account %x at height %d: %v", addr, v.blockView.blockHeight, err))
	}
	if account == nil || account.IsDelete() {
		return nil
	}
	return account
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

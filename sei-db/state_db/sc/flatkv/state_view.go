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
		account, ok := v.accountData(keyBytes)
		if !ok {
			return nil, false
		}
		return accountFieldValue(kind, account)

	case keys.EVMKeyStorage:
		storage, ok := v.storageData(keyBytes)
		if !ok {
			return nil, false
		}
		value := storage.GetValue()
		return value[:], true

	case keys.EVMKeyCode:
		code, ok := v.codeData(keyBytes)
		if !ok {
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
	_, ok := v.accountData(addr[:])
	return ok
}

// GetNonce returns addr's account nonce, or 0 when the account does not exist.
func (v *flatKVStateView) GetNonce(addr gigatypes.Address) uint64 {
	account, ok := v.accountData(addr[:])
	if !ok {
		return 0
	}
	return account.GetNonce()
}

// GetBalance returns addr's balance as a 256-bit big-endian value, or the zero value when addr holds
// no balance.
func (v *flatKVStateView) GetBalance(addr gigatypes.Address) gigatypes.Hash {
	account, ok := v.accountData(addr[:])
	if !ok {
		return gigatypes.Hash{}
	}
	return gigatypes.Hash(account.GetBalance())
}

// GetCodeHash returns the hash of addr's contract code, gigatypes.EmptyCodeHash when the account exists
// and holds no code, or the zero hash when it does not exist.
func (v *flatKVStateView) GetCodeHash(addr gigatypes.Address) gigatypes.Hash {
	account, ok := v.accountData(addr[:])
	if !ok {
		return gigatypes.Hash{}
	}
	codeHash := gigatypes.Hash(account.GetCodeHash())
	if codeHash == (gigatypes.Hash{}) {
		// A row only exists while some field is non-zero (see AccountData.IsDelete), and the code hash
		// is not that field here, so this account has a nonce or a balance and no code — the case EVM
		// semantics answer with the empty-code hash rather than with zero.
		return gigatypes.EmptyCodeHash
	}
	return codeHash
}

// ReadAccount returns addr's balance, nonce and code hash from one account row read.
func (v *flatKVStateView) ReadAccount(addr gigatypes.Address) (gigatypes.Account, bool) {
	account, ok := v.accountData(addr[:])
	if !ok {
		return gigatypes.Account{}, false
	}
	codeHash := gigatypes.Hash(account.GetCodeHash())
	if codeHash == (gigatypes.Hash{}) {
		// The row exists, so some field is non-zero and it is not this one: no code. See GetCodeHash.
		codeHash = gigatypes.EmptyCodeHash
	}
	return gigatypes.Account{
		Balance:  gigatypes.Hash(account.GetBalance()),
		Nonce:    account.GetNonce(),
		CodeHash: codeHash,
	}, true
}

// GetStorage returns the value at key in addr's storage, or the zero hash when the slot is unset.
func (v *flatKVStateView) GetStorage(addr gigatypes.Address, key gigatypes.Hash) gigatypes.Hash {
	storage, found := readEVMRow(v, v.blockView.StorageView(), keys.EVMKeyStorage, addr[:], key[:])
	if !found || storage.IsDelete() {
		return gigatypes.Hash{}
	}
	return gigatypes.Hash(storage.GetValue())
}

// GetCode returns addr's contract code, or nil when it has none. The slice aliases the store's row
// and is valid until the view is closed.
func (v *flatKVStateView) GetCode(addr gigatypes.Address) []byte {
	code, ok := v.codeData(addr[:])
	if !ok {
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

// accountData returns the account row for the 20-byte address in keyBytes, or false when no account
// exists in this block.
func (v *flatKVStateView) accountData(keyBytes []byte) (vtype.AccountData, bool) {
	account, found := readEVMRow(v, v.blockView.AccountView(), ktype.EVMKeyAccount, keyBytes)
	if !found || account.IsDelete() {
		return vtype.AccountData{}, false
	}
	return account, true
}

// storageData returns the storage row for the addr||slot in keyBytes, or false when the slot is unset.
func (v *flatKVStateView) storageData(keyBytes []byte) (vtype.StorageData, bool) {
	storage, found := readEVMRow(v, v.blockView.StorageView(), keys.EVMKeyStorage, keyBytes)
	if !found || storage.IsDelete() {
		return vtype.StorageData{}, false
	}
	return storage, true
}

// codeData returns the code row for the 20-byte address in keyBytes, or false when it has no code.
func (v *flatKVStateView) codeData(keyBytes []byte) (vtype.CodeData, bool) {
	code, found := readEVMRow(v, v.blockView.CodeView(), keys.EVMKeyCode, keyBytes)
	if !found || code.IsDelete() {
		return vtype.CodeData{}, false
	}
	return code, true
}

// miscValue returns the value stored under keyBytes in the named module, and whether it was found.
func (v *flatKVStateView) miscValue(module string, keyBytes []byte) ([]byte, bool) {
	misc, found := readRow(v, v.blockView.MiscView(), ktype.ModulePhysicalKey(module, keyBytes))
	if !found {
		return nil, false
	}
	return misc.GetValue(), true
}

// readEVMRow returns the row stored under an EVM physical key for kind and key parts.
func readEVMRow[V any](
	v *flatKVStateView,
	dbView view.View[V],
	kind keys.EVMKeyKind,
	keyParts ...[]byte,
) (V, bool) {
	buf := physKeyBufs.Get().(*[physKeyBufLen]byte)
	physKey := ktype.AppendEVMPhysicalKey(buf[:0], kind, keyParts[0])
	for _, keyPart := range keyParts[1:] {
		physKey = append(physKey, keyPart...)
	}
	value, found := readRow(v, dbView, physKey)
	// Not deferred: readRow panics when the manager shuts down while a read worker may still hold
	// physKey, and a buffer that may still be read must not go back to the pool.
	physKeyBufs.Put(buf)
	return value, found
}

// readRow returns the row stored under physKey.
func readRow[V any](v *flatKVStateView, dbView view.View[V], physKey []byte) (V, bool) {
	value, found, err := dbView.Get(physKey, true)
	if err != nil {
		panic(fmt.Sprintf("flatkv: %s read of key %x at height %d: %v",
			dbView.Name(), physKey, v.blockView.BlockHeight(), err))
	}
	return value, found
}

package flatkv

import (
	"fmt"
	"maps"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

var _ gigatypes.StateView = (*rowStateView)(nil)

// rowStore names the store a physical key's row lives in.
type rowStore int

const (
	accountRows rowStore = iota
	codeRows
	storageRows
	miscRows
	rowStoreCount
)

// rowReader reads the FlatKV rows of one block. Each read names the store the row lives in. A read it cannot serve
// panics.
type rowReader interface {
	// height returns the block the rows belong to.
	height() int64

	// getRow returns the bytes stored under physKey, a key of store, and whether they were found.
	getRow(store rowStore, physKey []byte) ([]byte, bool)

	// batchGetRows returns the bytes stored under each found key of physKeys, all keys of store, keyed by
	// string(key).
	batchGetRows(store rowStore, physKeys [][]byte) map[string][]byte

	// release frees what the rows hold. It is called once.
	release()
}

// rowStateView serves the Giga read API by decoding the FlatKV rows of one block.
type rowStateView struct {
	rows rowReader

	// Guards the release, so a second Close does not release rows this view no longer owns.
	closeOnce sync.Once

	// Closed by Close.
	closed utils.CloseMarker[rowStateView]
}

// newRowStateView returns a view decoding rows. The caller must Close it.
func newRowStateView(rows rowReader) *rowStateView {
	v := &rowStateView{rows: rows}
	v.closed = utils.MustClose(v, "flatkv state view")
	return v
}

// NewStateView returns a view of block height that decodes FlatKV rows, reading each by its physical key through
// read. read reports whether the key holds a row, and panics on a read it cannot serve. Close calls release.
func NewStateView(height int64, read func(physKey []byte) ([]byte, bool), release func()) gigatypes.StateView {
	return newRowStateView(&keyedRows{blockHeight: height, read: read, free: release})
}

// keyedRows reads rows through a function that finds any row by its physical key alone.
type keyedRows struct {
	blockHeight int64
	read        func(physKey []byte) ([]byte, bool)
	free        func()
}

func (r *keyedRows) height() int64 {
	return r.blockHeight
}

func (r *keyedRows) getRow(_ rowStore, physKey []byte) ([]byte, bool) {
	return r.read(physKey)
}

func (r *keyedRows) batchGetRows(_ rowStore, physKeys [][]byte) map[string][]byte {
	values := make(map[string][]byte, len(physKeys))
	for _, key := range physKeys {
		if value, found := r.read(key); found {
			values[string(key)] = value
		}
	}
	return values
}

func (r *keyedRows) release() {
	r.free()
}

// GetBlockHeight returns the block height of this view.
func (v *rowStateView) GetBlockHeight() int64 {
	return v.rows.height()
}

// Close releases the rows this view holds. The view must not be read afterwards. Idempotent.
func (v *rowStateView) Close() {
	v.closeOnce.Do(func() {
		v.closed.Close(v)
		v.rows.release()
	})
}

func (v *rowStateView) Get(module string, key []byte) ([]byte, bool) {
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

// GetRaw returns the bytes stored under a physical key in this block, and whether it was found. It
// panics on a key under the store's metadata prefix.
func (v *rowStateView) GetRaw(key []byte) ([]byte, bool) {
	store, err := v.storeFor(key)
	if err != nil {
		panic(err)
	}
	return v.rows.getRow(store, key)
}

// BatchGetRaw returns the bytes stored in this block under each found physical key, keyed by string(key).
// It panics on a key under the store's metadata prefix.
func (v *rowStateView) BatchGetRaw(keys [][]byte) map[string][]byte {
	var byStore [rowStoreCount][][]byte
	for k, key := range keys {
		store, err := v.storeFor(key)
		if err != nil {
			panic(err)
		}
		if byStore[store] == nil {
			// Sized for every remaining key, since a batch's keys usually all belong to one store.
			byStore[store] = make([][]byte, 0, len(keys)-k)
		}
		byStore[store] = append(byStore[store], key)
	}

	// Each store's answer is a map of its own, so the largest one collects the others rather than all of
	// them being copied into a new one. A batch within one store returns that store's answer as is.
	var values map[string][]byte
	for store, storeKeys := range byStore {
		if len(storeKeys) == 0 {
			continue
		}
		found := v.rows.batchGetRows(rowStore(store), storeKeys)
		if len(found) > len(values) {
			values, found = found, values
		}
		maps.Copy(values, found)
	}
	if values == nil {
		return map[string][]byte{}
	}
	return values
}

// AccountExists reports whether addr has an account in this block.
func (v *rowStateView) AccountExists(addr gigatypes.Address) bool {
	_, ok := v.accountRow(addr)
	return ok
}

// GetNonce returns addr's account nonce, or 0 when the account does not exist.
func (v *rowStateView) GetNonce(addr gigatypes.Address) uint64 {
	account, ok := v.accountRow(addr)
	if !ok {
		return 0
	}
	return account.Nonce()
}

// GetBalance returns addr's balance as a 256-bit big-endian value, or the zero value when addr holds
// no balance.
func (v *rowStateView) GetBalance(addr gigatypes.Address) gigatypes.Hash {
	account, ok := v.accountRow(addr)
	if !ok {
		return gigatypes.Hash{}
	}
	return gigatypes.Hash(account.Balance())
}

// GetCodeHash returns the hash of addr's contract code, gigatypes.EmptyCodeHash when the account exists
// and holds no code, or the zero hash when it does not exist.
func (v *rowStateView) GetCodeHash(addr gigatypes.Address) gigatypes.Hash {
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

// ReadAccount returns addr's balance, nonce and code hash from one account row read.
func (v *rowStateView) ReadAccount(addr gigatypes.Address) (gigatypes.Account, bool) {
	account, ok := v.accountRow(addr)
	if !ok {
		return gigatypes.Account{}, false
	}
	codeHash := gigatypes.Hash(account.CodeHash())
	if codeHash == (gigatypes.Hash{}) {
		// The row exists, so some field is non-zero and it is not this one: no code. See GetCodeHash.
		codeHash = gigatypes.EmptyCodeHash
	}
	return gigatypes.Account{
		Balance:  gigatypes.Hash(account.Balance()),
		Nonce:    account.Nonce(),
		CodeHash: codeHash,
	}, true
}

// GetStorage returns the value at key in addr's storage, or the zero hash when the slot is unset.
func (v *rowStateView) GetStorage(addr gigatypes.Address, key gigatypes.Hash) gigatypes.Hash {
	raw, found := v.readEVMRow(storageRows, keys.EVMKeyStorage, addr[:], key[:])
	if !found {
		return gigatypes.Hash{}
	}
	storage, err := vtype.ParseStorageRow(raw)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse storage %x/%x at height %d: %v",
			addr, key, v.rows.height(), err))
	}
	if storage.IsDelete() {
		return gigatypes.Hash{}
	}
	return gigatypes.Hash(storage.Value())
}

// GetCode returns addr's contract code, or nil when it has none. The slice aliases the store's row
// and is valid until the view is closed.
func (v *rowStateView) GetCode(addr gigatypes.Address) []byte {
	code := v.codeData(addr[:])
	if code == nil {
		return nil
	}
	return code.GetBytecode()
}

// GetCodeSize returns the length of addr's contract code in bytes, or 0 when it has none.
func (v *rowStateView) GetCodeSize(addr gigatypes.Address) int {
	return len(v.GetCode(addr))
}

// physKeyBufLen holds the longest EVM physical key: "evm/" + kind byte + address + slot.
const physKeyBufLen = len(keys.EVMStoreKey) + 2 + ktype.AddressLen + ktype.SlotLen

// physKeyBufs holds scratch buffers for building physical keys that live only for one read.
var physKeyBufs = sync.Pool{New: func() any { return new([physKeyBufLen]byte) }}

// accountRow returns addr's account row, or false when no account exists in this block. The row
// aliases store memory and is valid until the view is closed.
func (v *rowStateView) accountRow(addr gigatypes.Address) (vtype.AccountRow, bool) {
	raw, found := v.readEVMRow(accountRows, ktype.EVMKeyAccount, addr[:])
	if !found {
		return vtype.AccountRow{}, false
	}
	account, err := vtype.ParseAccountRow(raw)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse account %x at height %d: %v", addr, v.rows.height(), err))
	}
	if account.IsDelete() {
		return vtype.AccountRow{}, false
	}
	return account, true
}

// accountData returns the account row for the 20-byte address in keyBytes, or nil when no account
// exists in this block.
func (v *rowStateView) accountData(keyBytes []byte) *vtype.AccountData {
	raw, found := v.readEVMRow(accountRows, ktype.EVMKeyAccount, keyBytes)
	account, err := parseRow(raw, found, vtype.DeserializeAccountData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse account %x at height %d: %v",
			keyBytes, v.rows.height(), err))
	}
	if account == nil || account.IsDelete() {
		return nil
	}
	return account
}

// storageData returns the storage row for the addr||slot in keyBytes, or nil when the slot is unset.
func (v *rowStateView) storageData(keyBytes []byte) *vtype.StorageData {
	raw, found := v.readEVMRow(storageRows, keys.EVMKeyStorage, keyBytes)
	storage, err := parseRow(raw, found, vtype.DeserializeStorageData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse storage %x at height %d: %v",
			keyBytes, v.rows.height(), err))
	}
	if storage == nil || storage.IsDelete() {
		return nil
	}
	return storage
}

// codeData returns the code row for the 20-byte address in keyBytes, or nil when it has no code.
func (v *rowStateView) codeData(keyBytes []byte) *vtype.CodeData {
	raw, found := v.readEVMRow(codeRows, keys.EVMKeyCode, keyBytes)
	code, err := parseRow(raw, found, vtype.DeserializeCodeData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse code for %x at height %d: %v",
			keyBytes, v.rows.height(), err))
	}
	if code == nil || code.IsDelete() {
		return nil
	}
	return code
}

// miscValue returns the value stored under keyBytes in the named module, and whether it was found.
func (v *rowStateView) miscValue(module string, keyBytes []byte) ([]byte, bool) {
	raw, found := v.rows.getRow(miscRows, ktype.BuildPhysicalKey(module, keyBytes))
	misc, err := parseRow(raw, found, vtype.DeserializeMiscData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse misc %s/%x at height %d: %v",
			module, keyBytes, v.rows.height(), err))
	}
	if misc == nil || misc.IsDelete() {
		return nil, false
	}
	value := misc.GetValue()
	return value, value != nil
}

// readEVMRow returns the bytes stored in store under the EVM physical key for kind and key parts.
func (v *rowStateView) readEVMRow(store rowStore, kind keys.EVMKeyKind, keyParts ...[]byte) ([]byte, bool) {
	buf := physKeyBufs.Get().(*[physKeyBufLen]byte)
	physKey := ktype.WritePhysicalEVMKeyToBuffer(buf[:], kind, keyParts[0])
	for _, keyPart := range keyParts[1:] {
		physKey = append(physKey, keyPart...)
	}
	value, found := v.rows.getRow(store, physKey)
	// Not deferred: getRow panics when the manager shuts down while a read worker may still hold
	// physKey, and a buffer that may still be read must not go back to the pool.
	physKeyBufs.Put(buf)
	return value, found
}

// storeFor returns the store that holds physKey. It returns an error for a metadata key, since the store's own
// metadata is not part of any block's state, and for a key no store holds.
func (v *rowStateView) storeFor(physKey []byte) (rowStore, error) {
	if ktype.IsMetaKey(physKey) {
		return 0, fmt.Errorf("flatkv: raw key %x is store metadata, not block state", physKey)
	}
	dbDir, err := routePhysicalKey(physKey)
	if err != nil {
		return 0, fmt.Errorf("flatkv: route raw key %x at height %d: %w", physKey, v.rows.height(), err)
	}
	switch dbDir {
	case accountDBDir:
		return accountRows, nil
	case codeDBDir:
		return codeRows, nil
	case storageDBDir:
		return storageRows, nil
	case miscDBDir:
		return miscRows, nil
	default:
		return 0, fmt.Errorf("flatkv: raw key %x routes to unknown store %q", physKey, dbDir)
	}
}

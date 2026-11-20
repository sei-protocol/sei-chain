package flatkv

import (
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// RowReader returns the FlatKV row stored under a physical key, and whether one is. It has no recoverable
// errors: a read it cannot serve panics.
type RowReader interface {
	GetRaw(physicalKey []byte) ([]byte, bool)
}

var _ gigatypes.StateView = (*rowView)(nil)

// rowView serves the Giga read API at one block from FlatKV rows read through a RowReader, decoding them
// as a view of the live store decodes its own.
type rowView struct {
	height int64
	rows   RowReader

	// release frees whatever the view pins. Guarded by closeOnce.
	release   func()
	closeOnce sync.Once
}

// NewRowView returns a view of block blockHeight that decodes the FlatKV rows rows returns. Close calls
// release, which may be nil.
func NewRowView(blockHeight int64, rows RowReader, release func()) gigatypes.StateView {
	return &rowView{height: blockHeight, rows: rows, release: release}
}

func (v *rowView) GetBlockHeight() int64 {
	return v.height
}

func (v *rowView) Close() {
	v.closeOnce.Do(func() {
		if v.release != nil {
			v.release()
		}
	})
}

func (v *rowView) Get(module string, key []byte) ([]byte, bool) {
	if module != keys.EVMStoreKey {
		return v.miscValue(module, key)
	}

	kind, keyBytes := keys.ParseEVMKey(key)
	switch kind {
	case keys.EVMKeyEmpty:
		return nil, false

	case keys.EVMKeyNonce, keys.EVMKeyCodeHash, keys.EVMKeyBalance:
		account := decodeRow(v, kind, keyBytes, vtype.DeserializeAccountData)
		if account == nil || account.IsDelete() {
			return nil, false
		}
		return accountFieldValue(kind, account)

	case keys.EVMKeyStorage:
		storage := decodeRow(v, kind, keyBytes, vtype.DeserializeStorageData)
		if storage == nil || storage.IsDelete() {
			return nil, false
		}
		value := storage.GetValue()
		return value[:], true

	case keys.EVMKeyCode:
		code := decodeRow(v, kind, keyBytes, vtype.DeserializeCodeData)
		if code == nil || code.IsDelete() {
			return nil, false
		}
		return code.GetBytecode(), true

	case keys.EVMKeyMisc:
		return v.miscValue(keys.EVMStoreKey, keyBytes)

	default:
		panic(fmt.Sprintf("flatkv: Get unsupported key type: %v", kind))
	}
}

func (v *rowView) GetRaw(key []byte) ([]byte, bool) {
	return v.rows.GetRaw(key)
}

func (v *rowView) BatchGetRaw(keys [][]byte) map[string][]byte {
	values := make(map[string][]byte, len(keys))
	for _, key := range keys {
		if value, found := v.rows.GetRaw(key); found {
			values[string(key)] = value
		}
	}
	return values
}

func (v *rowView) AccountExists(addr gigatypes.Address) bool {
	_, ok := v.accountRow(addr)
	return ok
}

func (v *rowView) GetNonce(addr gigatypes.Address) uint64 {
	account, ok := v.accountRow(addr)
	if !ok {
		return 0
	}
	return account.Nonce()
}

func (v *rowView) GetBalance(addr gigatypes.Address) gigatypes.Hash {
	account, ok := v.accountRow(addr)
	if !ok {
		return gigatypes.Hash{}
	}
	return gigatypes.Hash(account.Balance())
}

func (v *rowView) GetCodeHash(addr gigatypes.Address) gigatypes.Hash {
	account, ok := v.ReadAccount(addr)
	if !ok {
		return gigatypes.Hash{}
	}
	return account.CodeHash
}

func (v *rowView) ReadAccount(addr gigatypes.Address) (gigatypes.Account, bool) {
	account, ok := v.accountRow(addr)
	if !ok {
		return gigatypes.Account{}, false
	}
	codeHash := gigatypes.Hash(account.CodeHash())
	if codeHash == (gigatypes.Hash{}) {
		// The row exists, so some field is non-zero and it is not this one: no code.
		codeHash = gigatypes.EmptyCodeHash
	}
	return gigatypes.Account{
		Balance:  gigatypes.Hash(account.Balance()),
		Nonce:    account.Nonce(),
		CodeHash: codeHash,
	}, true
}

func (v *rowView) GetStorage(addr gigatypes.Address, key gigatypes.Hash) gigatypes.Hash {
	raw, found := v.rows.GetRaw(ktype.BuildPhysicalEVMKey(keys.EVMKeyStorage, append(addr[:], key[:]...)))
	if !found {
		return gigatypes.Hash{}
	}
	storage, err := vtype.ParseStorageRow(raw)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse storage %x/%x at height %d: %v", addr, key, v.height, err))
	}
	if storage.IsDelete() {
		return gigatypes.Hash{}
	}
	return gigatypes.Hash(storage.Value())
}

func (v *rowView) GetCode(addr gigatypes.Address) []byte {
	code := decodeRow(v, keys.EVMKeyCode, addr[:], vtype.DeserializeCodeData)
	if code == nil || code.IsDelete() {
		return nil
	}
	return code.GetBytecode()
}

func (v *rowView) GetCodeSize(addr gigatypes.Address) int {
	return len(v.GetCode(addr))
}

// accountRow returns addr's account row, or false when no account exists at the view's block.
func (v *rowView) accountRow(addr gigatypes.Address) (vtype.AccountRow, bool) {
	raw, found := v.rows.GetRaw(ktype.BuildPhysicalEVMKey(ktype.EVMKeyAccount, addr[:]))
	if !found {
		return vtype.AccountRow{}, false
	}
	account, err := vtype.ParseAccountRow(raw)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse account %x at height %d: %v", addr, v.height, err))
	}
	if account.IsDelete() {
		return vtype.AccountRow{}, false
	}
	return account, true
}

// miscValue returns the value stored under keyBytes in the named module, and whether it was found.
func (v *rowView) miscValue(module string, keyBytes []byte) ([]byte, bool) {
	raw, found := v.rows.GetRaw(ktype.BuildPhysicalKey(module, keyBytes))
	misc, err := parseRow(raw, found, vtype.DeserializeMiscData)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse misc %s/%x at height %d: %v", module, keyBytes, v.height, err))
	}
	if misc == nil || misc.IsDelete() {
		return nil, false
	}
	value := misc.GetValue()
	return value, value != nil
}

// decodeRow reads and decodes the EVM row of kind for keyBytes, nil when there is none.
func decodeRow[T vtype.VType](v *rowView, kind keys.EVMKeyKind, keyBytes []byte, parse func([]byte) (T, error)) T {
	raw, found := v.rows.GetRaw(ktype.BuildPhysicalEVMKey(kind, keyBytes))
	row, err := parseRow(raw, found, parse)
	if err != nil {
		panic(fmt.Sprintf("flatkv: parse %v row %x at height %d: %v", kind, keyBytes, v.height, err))
	}
	return row
}

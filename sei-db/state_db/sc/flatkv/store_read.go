package flatkv

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// OpenView returns a read-only view of the most recently committed block. It is the Giga StateDB entry
// point for reads served out of SC. The caller must Close the view, which is what releases the
// reservation holding the block readable.
func (s *CommitStore) OpenView() gigatypes.StateView {
	blockView, err := s.lastSealed.Get()
	if err != nil {
		panic(fmt.Sprintf("flatkv: OpenView: %v", err))
	}
	v := &flatKVStateView{blockView: blockView}
	v.closed = utils.MustClose(v, "flatkv state view")
	return v
}

// Get returns the value for the given key within the specified module.
// For EVM keys (moduleName == keys.EVMStoreKey), the key is a prefix-encoded
// EVM key routed internally to account/storage/code/misc DBs.
// For non-EVM modules, the key is read from misc storage with the module prefix.
// Returns (value, true) if found, (nil, false) if not found.
// Panics on I/O errors or unsupported key types.
func (s *CommitStore) Get(moduleName string, key []byte) ([]byte, bool) {
	// Read lock: the internal getters (getAccountData, getStorageData,
	// getCodeData, getMiscData) read the pending-writes maps, which
	// ApplyChangeSets/Commit mutate under the write lock. Has delegates to Get
	// and must not take its own lock (RWMutex read locks are not reentrant).
	s.mu.RLock()
	defer s.mu.RUnlock()

	if moduleName != keys.EVMStoreKey {
		value, err := s.getMiscValue(moduleName, key)
		if err != nil {
			panic(fmt.Sprintf("flatkv: Get module=%s key %x: %v", moduleName, key, err))
		}
		return value, value != nil
	}

	kind, keyBytes := keys.ParseEVMKey(key)

	switch kind {
	case keys.EVMKeyEmpty:
		return nil, false
	case keys.EVMKeyStorage:
		value, err := s.getStorageValue(keyBytes)
		if err != nil {
			panic(fmt.Sprintf("flatkv: Get storage key %x: %v", key, err))
		}
		return value, value != nil

	case keys.EVMKeyNonce, keys.EVMKeyCodeHash, keys.EVMKeyBalance:
		accountData, found, err := s.getAccountData(keyBytes)
		if err != nil {
			panic(fmt.Sprintf("flatkv: Get account key %x: %v", key, err))
		}
		if !found || accountData.IsDelete() {
			return nil, false
		}
		return accountFieldValue(kind, accountData)

	case keys.EVMKeyCode:
		value, err := s.getCodeValue(keyBytes)
		if err != nil {
			panic(fmt.Sprintf("flatkv: Get code key %x: %v", key, err))
		}
		return value, value != nil

	case keys.EVMKeyMisc:
		value, err := s.getMiscValue(keys.EVMStoreKey, keyBytes)
		if err != nil {
			panic(fmt.Sprintf("flatkv: Get misc key %x: %v", key, err))
		}
		return value, value != nil

	default:
		panic(fmt.Sprintf("flatkv: Get unsupported key type: %v", kind))
	}
}

// GetBlockHeightModified returns the block height at which the key was last modified.
// Only supported for EVM keys; non-EVM misc data does not track block height.
// If not found, returns (-1, false, nil).
func (s *CommitStore) GetBlockHeightModified(moduleName string, key []byte) (int64, bool, error) {
	// Read lock: the internal getters (getStorageData, getAccountData,
	// getCodeData) read the pending-writes maps mutated under the write lock.
	s.mu.RLock()
	defer s.mu.RUnlock()

	if moduleName != keys.EVMStoreKey {
		return -1, false, fmt.Errorf("block height modified not tracked for module %q", moduleName)
	}

	kind, keyBytes := keys.ParseEVMKey(key)

	switch kind {
	case keys.EVMKeyStorage:
		sd, found, err := s.getStorageData(keyBytes)
		if err != nil {
			return -1, false, err
		}
		if !found || sd.IsDelete() {
			return -1, false, nil
		}
		return int64(sd.GetBlockHeight()), true, nil //nolint:gosec // written from an int64 height

	case keys.EVMKeyNonce, keys.EVMKeyCodeHash, keys.EVMKeyBalance:
		accountData, found, err := s.getAccountData(keyBytes)
		if err != nil {
			return -1, false, err
		}
		if !found || accountData.IsDelete() {
			return -1, false, nil
		}
		return int64(accountData.GetBlockHeight()), true, nil //nolint:gosec // written from an int64 height

	case keys.EVMKeyCode:
		cd, found, err := s.getCodeData(keyBytes)
		if err != nil {
			return -1, false, err
		}
		if !found || cd.IsDelete() {
			return -1, false, nil
		}
		return int64(cd.GetBlockHeight()), true, nil //nolint:gosec // written from an int64 height
	default:
		return -1, false, fmt.Errorf("block height modified not tracked for key type: %v", kind)
	}
}

// Has reports whether the key exists within the given module.
// Panics on I/O errors or unsupported key types.
func (s *CommitStore) Has(moduleName string, key []byte) bool {
	_, found := s.Get(moduleName, key)
	return found
}

// =============================================================================
// Internal Getters
// =============================================================================
//
// Each of these reads through its store, which already merges the values staged by the block currently
// being applied over the on-disk data. A key absent from both, and a key that same block deleted
// earlier, both come back as not found; every caller below collapses those two cases anyway.

// accountFieldValue projects the field that kind names out of an account row, encoded the way the
// logical EVM key for that field carries it: eight big-endian bytes for a nonce, thirty-two for a code
// hash or a balance. The second return reports whether that field is set.
//
// A zero code hash and a zero balance both report false. A deletion is stored by zeroing the field
// rather than by removing anything (see mergeAccountUpdates), so answering "present" for a zero would
// hand back a key the block deleted. The nonce is the exception: it answers for every row that exists.
func accountFieldValue(kind keys.EVMKeyKind, account vtype.AccountData) ([]byte, bool) {
	switch kind {
	case keys.EVMKeyNonce:
		nonceBytes := make([]byte, vtype.NonceLen)
		binary.BigEndian.PutUint64(nonceBytes, account.GetNonce())
		return nonceBytes, true

	case keys.EVMKeyCodeHash:
		codeHash := account.GetCodeHash()
		if codeHash == (vtype.CodeHash{}) {
			return nil, false
		}
		return codeHash[:], true

	case keys.EVMKeyBalance:
		balance := account.GetBalance()
		if balance == (vtype.Balance{}) {
			return nil, false
		}
		return balance[:], true

	default:
		panic(fmt.Sprintf("flatkv: %v does not name an account field", kind))
	}
}

func (s *CommitStore) getAccountData(keyBytes []byte) (vtype.AccountData, bool, error) {
	if len(keyBytes) != ktype.AddressLen {
		return vtype.AccountData{}, false,
			fmt.Errorf("accountDB: expected key length %d, got %d", ktype.AddressLen, len(keyBytes))
	}
	physKey := ktype.EVMPhysicalKey(ktype.EVMKeyAccount, keyBytes)
	account, found, err := s.accountStore.Get(physKey, true)
	if err != nil {
		return vtype.AccountData{}, false, fmt.Errorf("accountDB read of key %x: %w", physKey, err)
	}
	return account, found, nil
}

func (s *CommitStore) getStorageData(keyBytes []byte) (vtype.StorageData, bool, error) {
	if len(keyBytes) != ktype.AddressLen+ktype.SlotLen {
		return vtype.StorageData{}, false, fmt.Errorf("storageDB: expected key length %d, got %d",
			ktype.AddressLen+ktype.SlotLen, len(keyBytes))
	}
	physKey := ktype.EVMPhysicalKey(keys.EVMKeyStorage, keyBytes)
	storage, found, err := s.storageStore.Get(physKey, true)
	if err != nil {
		return vtype.StorageData{}, false, fmt.Errorf("storageDB read of key %x: %w", physKey, err)
	}
	return storage, found, nil
}

func (s *CommitStore) getStorageValue(key []byte) ([]byte, error) {
	sd, found, err := s.getStorageData(key)
	if err != nil {
		return nil, err
	}
	if !found || sd.IsDelete() {
		return nil, nil
	}
	value := sd.GetValue()
	return value[:], nil
}

func (s *CommitStore) getCodeData(keyBytes []byte) (vtype.CodeData, bool, error) {
	if len(keyBytes) != ktype.AddressLen {
		return vtype.CodeData{}, false,
			fmt.Errorf("codeDB: expected key length %d, got %d", ktype.AddressLen, len(keyBytes))
	}
	physKey := ktype.EVMPhysicalKey(keys.EVMKeyCode, keyBytes)
	code, found, err := s.codeStore.Get(physKey, true)
	if err != nil {
		return vtype.CodeData{}, false, fmt.Errorf("codeDB read of key %x: %w", physKey, err)
	}
	return code, found, nil
}

func (s *CommitStore) getCodeValue(key []byte) ([]byte, error) {
	cd, found, err := s.getCodeData(key)
	if err != nil {
		return nil, err
	}
	if !found || cd.IsDelete() {
		return nil, nil
	}
	return cd.GetBytecode(), nil
}

func (s *CommitStore) getMiscData(moduleName string, keyBytes []byte) (vtype.MiscData, bool, error) {
	physKey := ktype.ModulePhysicalKey(moduleName, keyBytes)
	misc, found, err := s.miscStore.Get(physKey, true)
	if err != nil {
		return vtype.MiscData{}, false, fmt.Errorf("miscDB read of key %x: %w", physKey, err)
	}
	return misc, found, nil
}

// getMiscValue returns the value stored under key in the named module, or nil when it holds none. A found
// value is never nil, even when empty.
func (s *CommitStore) getMiscValue(moduleName string, key []byte) ([]byte, error) {
	misc, found, err := s.getMiscData(moduleName, key)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return misc.GetValue(), nil
}

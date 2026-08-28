package flatkv

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// OpenView returns a read-only view of the most recently committed block. It is the Giga StateDB entry
// point for reads served out of SC. The caller must Close the view, which is what hands back the
// reservation holding the block readable.
func (s *CommitStore) OpenView() giga.StateView {
	blockView, err := s.lastSealed.get()
	if err != nil {
		panic(fmt.Sprintf("flatkv: OpenView: %v", err))
	}
	return &flatKVStateView{blockView: blockView}
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

	case keys.EVMKeyNonce, keys.EVMKeyCodeHash:
		accountData, err := s.getAccountData(keyBytes)
		if err != nil {
			panic(fmt.Sprintf("flatkv: Get account key %x: %v", key, err))
		}
		if accountData == nil || accountData.IsDelete() {
			return nil, false
		}

		if kind == keys.EVMKeyNonce {
			nonceBytes := make([]byte, vtype.NonceLen)
			binary.BigEndian.PutUint64(nonceBytes, accountData.GetNonce())
			return nonceBytes, true
		}
		// CodeHash
		codeHash := accountData.GetCodeHash()
		var zeroCodeHash vtype.CodeHash
		if *codeHash == zeroCodeHash {
			return nil, false
		}
		return codeHash[:], true

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
		sd, err := s.getStorageData(keyBytes)
		if err != nil {
			return -1, false, err
		}
		if sd == nil || sd.IsDelete() {
			return -1, false, nil
		}
		return sd.GetBlockHeight(), true, nil

	case keys.EVMKeyNonce, keys.EVMKeyCodeHash:
		accountData, err := s.getAccountData(keyBytes)
		if err != nil {
			return -1, false, err
		}
		if accountData == nil || accountData.IsDelete() {
			return -1, false, nil
		}
		return accountData.GetBlockHeight(), true, nil

	case keys.EVMKeyCode:
		cd, err := s.getCodeData(keyBytes)
		if err != nil {
			return -1, false, err
		}
		if cd == nil || cd.IsDelete() {
			return -1, false, nil
		}
		return cd.GetBlockHeight(), true, nil
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
// earlier, both come back as the zero value; every caller below collapses those two cases anyway.

// parseRow deserializes a row read out of a store, or returns the zero value of T when the row was
// not there.
//
// A key that the block currently being applied has already deleted reads as absent rather than as a
// tombstone, so callers need not distinguish "never existed" from "deleted by the block in progress" —
// both yield the zero value, which every FlatKV read path already treats the same way as a value whose
// IsDelete reports true.
func parseRow[T vtype.VType](raw []byte, found bool, parse func([]byte) (T, error)) (T, error) {
	var zero T
	if !found {
		return zero, nil
	}
	return parse(raw)
}

func (s *CommitStore) getAccountData(keyBytes []byte) (*vtype.AccountData, error) {
	if len(keyBytes) != ktype.AddressLen {
		return nil, fmt.Errorf("accountDB: expected key length %d, got %d", ktype.AddressLen, len(keyBytes))
	}
	physKey := ktype.EVMPhysicalKey(ktype.EVMKeyAccount, keyBytes)
	raw, found, err := s.accountStore.Get(physKey, true)
	if err != nil {
		return nil, fmt.Errorf("accountDB read of key %x: %w", physKey, err)
	}
	return parseRow(raw, found, vtype.DeserializeAccountData)
}

func (s *CommitStore) getStorageData(keyBytes []byte) (*vtype.StorageData, error) {
	if len(keyBytes) != ktype.AddressLen+ktype.SlotLen {
		return nil, fmt.Errorf("storageDB: expected key length %d, got %d", ktype.AddressLen+ktype.SlotLen, len(keyBytes))
	}
	physKey := ktype.EVMPhysicalKey(keys.EVMKeyStorage, keyBytes)
	raw, found, err := s.storageStore.Get(physKey, true)
	if err != nil {
		return nil, fmt.Errorf("storageDB read of key %x: %w", physKey, err)
	}
	return parseRow(raw, found, vtype.DeserializeStorageData)
}

func (s *CommitStore) getStorageValue(key []byte) ([]byte, error) {
	sd, err := s.getStorageData(key)
	if err != nil {
		return nil, err
	}
	if sd == nil || sd.IsDelete() {
		return nil, nil
	}
	return sd.GetValue()[:], nil
}

func (s *CommitStore) getCodeData(keyBytes []byte) (*vtype.CodeData, error) {
	if len(keyBytes) != ktype.AddressLen {
		return nil, fmt.Errorf("codeDB: expected key length %d, got %d", ktype.AddressLen, len(keyBytes))
	}
	physKey := ktype.EVMPhysicalKey(keys.EVMKeyCode, keyBytes)
	raw, found, err := s.codeStore.Get(physKey, true)
	if err != nil {
		return nil, fmt.Errorf("codeDB read of key %x: %w", physKey, err)
	}
	return parseRow(raw, found, vtype.DeserializeCodeData)
}

func (s *CommitStore) getCodeValue(key []byte) ([]byte, error) {
	cd, err := s.getCodeData(key)
	if err != nil {
		return nil, err
	}
	if cd == nil || cd.IsDelete() {
		return nil, nil
	}
	return cd.GetBytecode(), nil
}

func (s *CommitStore) getMiscData(moduleName string, keyBytes []byte) (*vtype.MiscData, error) {
	physKey := ktype.ModulePhysicalKey(moduleName, keyBytes)
	raw, found, err := s.miscStore.Get(physKey, true)
	if err != nil {
		return nil, fmt.Errorf("miscDB read of key %x: %w", physKey, err)
	}
	return parseRow(raw, found, vtype.DeserializeMiscData)
}

func (s *CommitStore) getMiscValue(moduleName string, key []byte) ([]byte, error) {
	ld, err := s.getMiscData(moduleName, key)
	if err != nil {
		return nil, err
	}
	if ld == nil || ld.IsDelete() {
		return nil, nil
	}
	return ld.GetValue(), nil
}

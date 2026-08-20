package flatkv

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/snapshot"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// Get returns the value for the given key within the specified module.
// For EVM keys (moduleName == keys.EVMStoreKey), the key is a prefix-encoded
// EVM key routed internally to account/storage/code/misc DBs.
// For non-EVM modules, the key is read from misc storage with the module prefix.
// Returns (value, true) if found, (nil, false) if not found.
// Panics on I/O errors or unsupported key types.
func (s *CommitStore) Get(moduleName string, key []byte) ([]byte, bool) {
	// Unsynchronized: the getters reach only into the snapshot engines, which synchronize their own
	// reads and serve the current mutable version, so a block's uncommitted writes are read safely.
	// Correct only while no reopen overlaps a read, since openStores and closeStores reassign the
	// store fields.
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
	// Unsynchronized, for the same reason as Get: the getters reach only into the snapshot engines,
	// which synchronize their own reads.
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

// getAndParse returns the value stored under physKey, deserialized, or the zero value of T when the
// key is absent.
//
// A key that the block currently being applied has already deleted reads as absent rather than as a
// tombstone, so callers need not distinguish "never existed" from "deleted by the block in progress" —
// both yield the zero value, which every FlatKV read path already treats the same way as a value whose
// IsDelete reports true.
func getAndParse[T vtype.VType](
	store snapshot.SnapshotEngine,
	physKey []byte,
	parse func([]byte) (T, error),
) (T, error) {
	var zero T
	raw, found, err := store.Get(physKey, true)
	if err != nil {
		return zero, fmt.Errorf("%s read of key %x: %w", store.Name(), physKey, err)
	}
	if !found {
		return zero, nil
	}
	return parse(raw)
}

func (s *CommitStore) getAccountData(keyBytes []byte) (*vtype.AccountData, error) {
	if len(keyBytes) != ktype.AddressLen {
		return nil, fmt.Errorf("accountDB: expected key length %d, got %d", ktype.AddressLen, len(keyBytes))
	}
	return getAndParse(s.accountStore, ktype.EVMPhysicalKey(ktype.EVMKeyAccount, keyBytes),
		vtype.DeserializeAccountData)
}

func (s *CommitStore) getStorageData(keyBytes []byte) (*vtype.StorageData, error) {
	if len(keyBytes) != ktype.AddressLen+ktype.SlotLen {
		return nil, fmt.Errorf("storageDB: expected key length %d, got %d", ktype.AddressLen+ktype.SlotLen, len(keyBytes))
	}
	return getAndParse(s.storageStore, ktype.EVMPhysicalKey(keys.EVMKeyStorage, keyBytes),
		vtype.DeserializeStorageData)
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
	return getAndParse(s.codeStore, ktype.EVMPhysicalKey(keys.EVMKeyCode, keyBytes),
		vtype.DeserializeCodeData)
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
	return getAndParse(s.miscStore, ktype.ModulePhysicalKey(moduleName, keyBytes),
		vtype.DeserializeMiscData)
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

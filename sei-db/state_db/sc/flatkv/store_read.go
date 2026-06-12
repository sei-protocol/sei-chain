package flatkv

import (
	"encoding/binary"
	"fmt"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// Get returns the value for the given key within the specified module.
// For EVM keys (moduleName == keys.EVMStoreKey), the key is a prefix-encoded
// EVM key routed internally to account/storage/code/legacy DBs.
// For non-EVM modules, the key is read from legacy storage with the module prefix.
// Returns (value, true) if found, (nil, false) if not found.
// Panics on I/O errors or unsupported key types.
func (s *CommitStore) Get(moduleName string, key []byte) ([]byte, bool) {
	// Read lock: the internal getters (getAccountData, getStorageData,
	// getCodeData, getLegacyData) read the pending-writes maps, which
	// ApplyChangeSets/Commit mutate under the write lock. Has delegates to Get
	// and must not take its own lock (RWMutex read locks are not reentrant).
	s.mu.RLock()
	defer s.mu.RUnlock()

	if moduleName != keys.EVMStoreKey {
		value, err := s.getLegacyValue(moduleName, key)
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

	case keys.EVMKeyLegacy:
		value, err := s.getLegacyValue(keys.EVMStoreKey, keyBytes)
		if err != nil {
			panic(fmt.Sprintf("flatkv: Get legacy key %x: %v", key, err))
		}
		return value, value != nil

	default:
		panic(fmt.Sprintf("flatkv: Get unsupported key type: %v", kind))
	}
}

// GetBlockHeightModified returns the block height at which the key was last modified.
// Only supported for EVM keys; non-EVM legacy data does not track block height.
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
// Internal Getters (used by ApplyChangeSets for LtHash computation)
// =============================================================================

// readFromDB checks pending writes first, then falls back to a DB read.
// Returns (zero, nil) when the key is not found.
func readFromDB[T vtype.VType](
	physKey []byte,
	pendingWrites map[string]T,
	db seidbtypes.KeyValueDB,
	deserialize func([]byte) (T, error),
	dbName string,
) (T, error) {
	if v, ok := pendingWrites[string(physKey)]; ok {
		return v, nil
	}
	raw, err := db.Get(physKey)
	if err != nil {
		var zero T
		if errorutils.IsNotFound(err) {
			return zero, nil
		}
		return zero, fmt.Errorf("%s I/O error for key %x: %w", dbName, physKey, err)
	}
	return deserialize(raw)
}

func (s *CommitStore) getAccountData(keyBytes []byte) (*vtype.AccountData, error) {
	if len(keyBytes) != ktype.AddressLen {
		return nil, fmt.Errorf("accountDB: expected key length %d, got %d", ktype.AddressLen, len(keyBytes))
	}
	return readFromDB(ktype.EVMPhysicalKey(ktype.EVMKeyAccount, keyBytes), s.accountWrites, s.accountDB, vtype.DeserializeAccountData, "accountDB")
}

func (s *CommitStore) getStorageData(keyBytes []byte) (*vtype.StorageData, error) {
	if len(keyBytes) != ktype.AddressLen+ktype.SlotLen {
		return nil, fmt.Errorf("storageDB: expected key length %d, got %d", ktype.AddressLen+ktype.SlotLen, len(keyBytes))
	}
	return readFromDB(ktype.EVMPhysicalKey(keys.EVMKeyStorage, keyBytes), s.storageWrites, s.storageDB, vtype.DeserializeStorageData, "storageDB")
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
	return readFromDB(ktype.EVMPhysicalKey(keys.EVMKeyCode, keyBytes), s.codeWrites, s.codeDB, vtype.DeserializeCodeData, "codeDB")
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

func (s *CommitStore) getLegacyData(moduleName string, keyBytes []byte) (*vtype.LegacyData, error) {
	return readFromDB(ktype.ModulePhysicalKey(moduleName, keyBytes), s.legacyWrites, s.legacyDB, vtype.DeserializeLegacyData, "legacyDB")
}

func (s *CommitStore) getLegacyValue(moduleName string, key []byte) ([]byte, error) {
	ld, err := s.getLegacyData(moduleName, key)
	if err != nil {
		return nil, err
	}
	if ld == nil || ld.IsDelete() {
		return nil, nil
	}
	return ld.GetValue(), nil
}

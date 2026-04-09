package flatkv

import (
	"bytes"
	"encoding/binary"
	"fmt"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// Get returns the value for the given memiavl key.
// Returns (value, true) if found, (nil, false) if not found.
// Panics on I/O errors or unsupported key types.
func (s *CommitStore) Get(key []byte) ([]byte, bool) {
	kind, keyBytes := evm.ParseEVMKey(key)

	switch kind {
	case evm.EVMKeyEmpty:
		// An empty key is always not found.
		return nil, false
	case evm.EVMKeyStorage:
		value, err := s.getStorageValue(keyBytes)
		if err != nil {
			panic(fmt.Sprintf("flatkv: Get storage key %x: %v", key, err))
		}
		return value, value != nil

	case evm.EVMKeyNonce, evm.EVMKeyCodeHash:
		accountData, err := s.getAccountData(keyBytes)
		if err != nil {
			panic(fmt.Sprintf("flatkv: Get account key %x: %v", key, err))
		}
		if accountData == nil || accountData.IsDelete() {
			return nil, false
		}

		if kind == evm.EVMKeyNonce {
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

	case evm.EVMKeyCode:
		value, err := s.getCodeValue(keyBytes)
		if err != nil {
			panic(fmt.Sprintf("flatkv: Get code key %x: %v", key, err))
		}
		return value, value != nil

	case evm.EVMKeyLegacy:
		value, err := s.getLegacyValue(evm.EVMStoreKey, keyBytes)
		if err != nil {
			panic(fmt.Sprintf("flatkv: Get legacy key %x: %v", key, err))
		}
		return value, value != nil

	default:
		panic(fmt.Sprintf("flatkv: Get unsupported key type: %v", kind))
	}
}

// GetBlockHeightModified returns the block height at which the key was last modified.
// If not found, returns (-1, false, nil).
func (s *CommitStore) GetBlockHeightModified(key []byte) (int64, bool, error) {
	kind, keyBytes := evm.ParseEVMKey(key)

	switch kind {
	case evm.EVMKeyStorage:
		sd, err := s.getStorageData(keyBytes)
		if err != nil {
			return -1, false, err
		}
		if sd == nil || sd.IsDelete() {
			return -1, false, nil
		}
		return sd.GetBlockHeight(), true, nil

	case evm.EVMKeyNonce, evm.EVMKeyCodeHash:
		accountData, err := s.getAccountData(keyBytes)
		if err != nil {
			return -1, false, err
		}
		if accountData == nil || accountData.IsDelete() {
			return -1, false, nil
		}
		return accountData.GetBlockHeight(), true, nil

	case evm.EVMKeyCode:
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

// Has reports whether the given memiavl key exists.
// Panics on I/O errors or unsupported key types.
func (s *CommitStore) Has(key []byte) bool {
	_, found := s.Get(key)
	return found
}

// Iterator returns an iterator over [start, end) in memiavl key order.
//
// IMPORTANT: Iterator only reads COMMITTED state from the underlying DBs.
// Pending writes from ApplyChangeSets are NOT visible until after Commit().
//
// EXPERIMENTAL: not used in production; only storage keys (0x03) supported.
// Interface may change when Exporter/state-sync is implemented.
func (s *CommitStore) Iterator(start, end []byte) Iterator {
	// Validate bounds: start must be < end
	if start != nil && end != nil && bytes.Compare(start, end) >= 0 {
		return &emptyIterator{} // Invalid range [start, end)
	}

	// Check if start/end are storage keys before iterating storage
	if start != nil {
		kind, _ := evm.ParseEVMKey(start)
		if kind != evm.EVMKeyUnknown && kind != evm.EVMKeyStorage {
			return &emptyIterator{}
		}
	}
	if end != nil {
		kind, _ := evm.ParseEVMKey(end)
		if kind != evm.EVMKeyUnknown && kind != evm.EVMKeyStorage {
			return &emptyIterator{}
		}
	}

	return s.newStorageIterator(start, end)
}

// IteratorByPrefix returns an iterator for keys with the given prefix.
// More efficient than Iterator for single-address queries.
//
// IMPORTANT: Like Iterator(), this only reads COMMITTED state.
// Pending writes are not visible until Commit().
//
// EXPERIMENTAL: not used in production; only storage keys supported.
// Interface may change when Exporter/state-sync is implemented.
func (s *CommitStore) IteratorByPrefix(prefix []byte) Iterator {
	if len(prefix) == 0 {
		return s.Iterator(nil, nil)
	}

	// Handle storage address prefix specially.
	// ParseEVMKey requires full key length (prefix + addr + slot = 53 bytes),
	// but a storage prefix is only (prefix + addr = 21 bytes).
	// Detect storage prefix: 0x03 || addr(20) = 21 bytes
	statePrefix := evm.StateKeyPrefix()
	if len(prefix) == len(statePrefix)+AddressLen &&
		bytes.HasPrefix(prefix, statePrefix) {
		// Storage address prefix: iterate all slots for this address
		// Internal key format: addr(20) || slot(32)
		// For prefix scan: use addr(20) as prefix
		addrBytes := prefix[len(statePrefix):]
		return s.newStoragePrefixIterator(addrBytes, prefix)
	}

	// Try parsing as full key
	kind, keyBytes := evm.ParseEVMKey(prefix)
	if kind == evm.EVMKeyUnknown {
		// Invalid prefix, return empty iterator
		return &emptyIterator{}
	}

	switch kind {
	case evm.EVMKeyStorage:
		return s.newStoragePrefixIterator(keyBytes, prefix)

	case evm.EVMKeyNonce, evm.EVMKeyCodeHash, evm.EVMKeyCode:
		return &emptyIterator{}

	default:
		return &emptyIterator{}
	}
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
	if len(keyBytes) != AddressLen {
		return nil, nil
	}
	return readFromDB(EVMPhysicalKey(EVMKeyAccount, keyBytes), s.accountWrites, s.accountDB, vtype.DeserializeAccountData, "accountDB")
}

func (s *CommitStore) getStorageData(keyBytes []byte) (*vtype.StorageData, error) {
	if len(keyBytes) != AddressLen+SlotLen {
		return nil, nil
	}
	return readFromDB(EVMPhysicalKey(evm.EVMKeyStorage, keyBytes), s.storageWrites, s.storageDB, vtype.DeserializeStorageData, "storageDB")
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
	if len(keyBytes) != AddressLen {
		return nil, nil
	}
	return readFromDB(EVMPhysicalKey(evm.EVMKeyCode, keyBytes), s.codeWrites, s.codeDB, vtype.DeserializeCodeData, "codeDB")
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
	return readFromDB(ModulePhysicalKey(moduleName, keyBytes), s.legacyWrites, s.legacyDB, vtype.DeserializeLegacyData, "legacyDB")
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

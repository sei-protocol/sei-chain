package flatkv

import (
	"bytes"
	"encoding/binary"
	"fmt"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// Get returns the value for the given memiavl key.
// Returns (value, true, nil) if found, (nil, false, nil) if not found.
func (s *CommitStore) Get(key []byte) ([]byte, bool, error) {
	kind, keyBytes := evm.ParseEVMKey(key)
	if !IsSupportedKeyType(kind) {
		if s.config.StrictKeyTypeCheck {
			return nil, false, fmt.Errorf("unsupported key type: %v", kind)
		}
		logger.Warn("unsupported key type in Get", "kind", kind)
		return nil, false, nil
	}

	switch kind {
	case evm.EVMKeyStorage:
		value, err := s.getStorageValue(keyBytes)
		if err != nil {
			return nil, false, err
		}
		return value, value != nil, nil

	case evm.EVMKeyNonce, evm.EVMKeyCodeHash:
		accountData, err := s.getAccountData(keyBytes)
		if err != nil {
			return nil, false, err
		}
		if accountData == nil || accountData.IsDelete() {
			return nil, false, nil
		}

		if kind == evm.EVMKeyNonce {
			nonceBytes := make([]byte, vtype.NonceLen)
			binary.BigEndian.PutUint64(nonceBytes, accountData.GetNonce())
			return nonceBytes, true, nil
		}
		// CodeHash
		codeHash := accountData.GetCodeHash()
		var zeroCodeHash vtype.CodeHash
		if *codeHash == zeroCodeHash {
			return nil, false, nil
		}
		return codeHash[:], true, nil

	case evm.EVMKeyCode:
		value, err := s.getCodeValue(keyBytes)
		if err != nil {
			return nil, false, err
		}
		return value, value != nil, nil

	case evm.EVMKeyLegacy:
		value, err := s.getLegacyValue(keyBytes)
		if err != nil {
			return nil, false, err
		}
		return value, value != nil, nil

	default:
		return nil, false, nil
	}
}

// GetBlockHeightModified returns the block height at which the key was last modified.
// If not found, returns (-1, false, nil).
func (s *CommitStore) GetBlockHeightModified(key []byte) (int64, bool, error) {
	kind, keyBytes := evm.ParseEVMKey(key)
	if !IsSupportedKeyType(kind) {
		if s.config.StrictKeyTypeCheck {
			return -1, false, fmt.Errorf("unsupported key type: %v", kind)
		}
		logger.Warn("unsupported key type in GetBlockHeightModified", "kind", kind)
		return -1, false, nil
	}

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

	case evm.EVMKeyLegacy:
		ld, err := s.getLegacyData(keyBytes)
		if err != nil {
			return -1, false, err
		}
		if ld == nil || ld.IsDelete() {
			return -1, false, nil
		}
		return ld.GetBlockHeight(), true, nil

	default:
		return -1, false, nil
	}
}

// Has reports whether the given memiavl key exists.
func (s *CommitStore) Has(key []byte) (bool, error) {
	_, found, err := s.Get(key)
	if err != nil {
		return false, fmt.Errorf("failed to get key %x: %w", key, err)
	}
	return found, nil
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

func (s *CommitStore) getAccountData(keyBytes []byte) (*vtype.AccountData, error) {
	addr, ok := AddressFromBytes(keyBytes)
	if !ok {
		return nil, nil
	}

	if accountValue, found := s.accountWrites[string(addr[:])]; found {
		return accountValue, nil
	}

	encoded, err := s.accountDB.Get(AccountKey(addr))
	if err != nil {
		if errorutils.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("accountDB I/O error for key %x: %w", addr, err)
	}
	return vtype.DeserializeAccountData(encoded)
}

func (s *CommitStore) getStorageData(keyBytes []byte) (*vtype.StorageData, error) {
	pendingWrite, hasPending := s.storageWrites[string(keyBytes)]
	if hasPending {
		return pendingWrite, nil
	}

	value, err := s.storageDB.Get(keyBytes)
	if err != nil {
		if errorutils.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("storageDB I/O error for key %x: %w", keyBytes, err)
	}
	return vtype.DeserializeStorageData(value)
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
	pendingWrite, hasPending := s.codeWrites[string(keyBytes)]
	if hasPending {
		return pendingWrite, nil
	}

	value, err := s.codeDB.Get(keyBytes)
	if err != nil {
		if errorutils.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("codeDB I/O error for key %x: %w", keyBytes, err)
	}
	return vtype.DeserializeCodeData(value)
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

func (s *CommitStore) getLegacyData(keyBytes []byte) (*vtype.LegacyData, error) {
	pendingWrite, hasPending := s.legacyWrites[string(keyBytes)]
	if hasPending {
		return pendingWrite, nil
	}

	value, err := s.legacyDB.Get(keyBytes)
	if err != nil {
		if errorutils.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("legacyDB I/O error for key %x: %w", keyBytes, err)
	}
	return vtype.DeserializeLegacyData(value)
}

func (s *CommitStore) getLegacyValue(key []byte) ([]byte, error) {
	ld, err := s.getLegacyData(key)
	if err != nil {
		return nil, err
	}
	if ld == nil || ld.IsDelete() {
		return nil, nil
	}
	return ld.GetValue(), nil
}

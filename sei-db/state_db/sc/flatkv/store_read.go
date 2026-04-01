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
// Returns (value, true) if found, (nil, false) if not found.
func (s *CommitStore) Get(key []byte) ([]byte, bool) {
	kind, keyBytes := evm.ParseEVMKey(key)
	if kind == evm.EVMKeyUnknown {
		return nil, false
	}

	switch kind {
	case evm.EVMKeyStorage:
		value, err := s.getStorageValue(keyBytes)
		if err != nil {
			return nil, false
		}
		return value, value != nil

	case evm.EVMKeyNonce, evm.EVMKeyCodeHash:
		// Account data: keyBytes = addr(20)
		// accountDB stores AccountValue at key=addr(20)
		addr, ok := AddressFromBytes(keyBytes)
		if !ok {
			return nil, false
		}

		// Check pending writes first
		if accountValue, found := s.accountWrites[string(addr[:])]; found {
			if accountValue.IsDelete() {
				return nil, false
			}
			if kind == evm.EVMKeyNonce {
				nonceBytes := make([]byte, vtype.NonceLen)
				binary.BigEndian.PutUint64(nonceBytes, accountValue.GetNonce())
				return nonceBytes, true
			}
			// CodeHash
			codeHash := accountValue.GetCodeHash()
			var zeroCodeHash vtype.CodeHash
			if *codeHash == zeroCodeHash {
				return nil, false
			}
			return codeHash[:], true
		}

		// Read from accountDB
		encoded, err := s.accountDB.Get(AccountKey(addr))
		if err != nil {
			return nil, false
		}
		accountData, err := vtype.DeserializeAccountData(encoded)
		if err != nil {
			return nil, false
		}

		if kind == evm.EVMKeyNonce {
			nonce := make([]byte, vtype.NonceLen)
			binary.BigEndian.PutUint64(nonce, accountData.GetNonce())
			return nonce, true
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
			return nil, false
		}
		return value, value != nil

	case evm.EVMKeyLegacy:
		value, err := s.getLegacyValue(keyBytes)
		if err != nil {
			return nil, false
		}
		return value, value != nil

	default:
		return nil, false
	}
}

// Has reports whether the given memiavl key exists.
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

func (s *CommitStore) getStorageValue(key []byte) ([]byte, error) {
	pendingWrite, hasPending := s.storageWrites[string(key)]
	if hasPending {
		if pendingWrite.IsDelete() {
			return nil, nil
		}
		return pendingWrite.GetValue()[:], nil
	}

	value, err := s.storageDB.Get(key)
	if err != nil {
		if errorutils.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("storageDB I/O error for key %x: %w", key, err)
	}

	storageData, err := vtype.DeserializeStorageData(value)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize storage data: %w", err)
	}
	return storageData.GetValue()[:], nil
}

func (s *CommitStore) getCodeValue(key []byte) ([]byte, error) {
	pendingWrite, hasPending := s.codeWrites[string(key)]
	if hasPending {
		return pendingWrite.GetBytecode(), nil
	}

	value, err := s.codeDB.Get(key)
	if err != nil {
		if errorutils.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("codeDB I/O error for key %x: %w", key, err)
	}

	codeData, err := vtype.DeserializeCodeData(value)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize code data: %w", err)
	}
	return codeData.GetBytecode(), nil
}

func (s *CommitStore) getLegacyValue(key []byte) ([]byte, error) {
	pendingWrite, hasPending := s.legacyWrites[string(key)]
	if hasPending {
		if pendingWrite.IsDelete() {
			return nil, nil
		}
		return pendingWrite.GetValue(), nil
	}

	value, err := s.legacyDB.Get(key)
	if err != nil {
		if errorutils.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("legacyDB I/O error for key %x: %w", key, err)
	}

	legacyData, err := vtype.DeserializeLegacyData(value)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize legacy data: %w", err)
	}
	return legacyData.GetValue(), nil
}

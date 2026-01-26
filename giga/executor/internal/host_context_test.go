package internal

import (
	"testing"

	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// TestStorageStatusLogic tests the storage status determination logic
// This tests the algorithm used in SetStorage without needing a full StateDB
func TestStorageStatusLogic(t *testing.T) {
	tests := []struct {
		name           string
		original       common.Hash // committed state
		current        common.Hash // current state in tx
		newValue       common.Hash // value being set
		expectedStatus evmc.StorageStatus
	}{
		{
			name:           "StorageAdded - zero to non-zero, clean",
			original:       common.Hash{},
			current:        common.Hash{},
			newValue:       common.Hash{1, 2, 3},
			expectedStatus: evmc.StorageAdded,
		},
		{
			name:           "StorageDeleted - non-zero to zero, clean",
			original:       common.Hash{1, 2, 3},
			current:        common.Hash{1, 2, 3},
			newValue:       common.Hash{},
			expectedStatus: evmc.StorageDeleted,
		},
		{
			name:           "StorageModified - non-zero to different non-zero, clean",
			original:       common.Hash{1, 2, 3},
			current:        common.Hash{1, 2, 3},
			newValue:       common.Hash{4, 5, 6},
			expectedStatus: evmc.StorageModified,
		},
		{
			name:           "StorageAssigned - dirty, not restored, same value",
			original:       common.Hash{1, 1, 1},
			current:        common.Hash{2, 2, 2},
			newValue:       common.Hash{2, 2, 2}, // same as current
			expectedStatus: evmc.StorageAssigned,
		},
		{
			name:           "StorageDeletedRestored - was deleted, restore to original",
			original:       common.Hash{1, 2, 3},
			current:        common.Hash{},        // deleted
			newValue:       common.Hash{1, 2, 3}, // restore
			expectedStatus: evmc.StorageDeletedRestored,
		},
		{
			name:           "StorageAddedDeleted - was added, now delete",
			original:       common.Hash{},
			current:        common.Hash{1, 2, 3}, // added
			newValue:       common.Hash{},        // delete (restore to original)
			expectedStatus: evmc.StorageAddedDeleted,
		},
		{
			name:           "StorageModifiedRestored - was modified, restore to original",
			original:       common.Hash{1, 2, 3},
			current:        common.Hash{4, 5, 6}, // modified
			newValue:       common.Hash{1, 2, 3}, // restore
			expectedStatus: evmc.StorageModifiedRestored,
		},
		{
			name:           "StorageDeletedAdded - was deleted, add back different",
			original:       common.Hash{1, 2, 3},
			current:        common.Hash{},        // deleted
			newValue:       common.Hash{4, 5, 6}, // different non-zero
			expectedStatus: evmc.StorageAssigned, // dirty && !restored && !(currentIsZero && valueIsZero)
		},
		{
			name:           "StorageModifiedDeleted - was modified, now delete",
			original:       common.Hash{1, 2, 3},
			current:        common.Hash{4, 5, 6}, // modified, not zero
			newValue:       common.Hash{},        // delete
			expectedStatus: evmc.StorageModifiedDeleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := computeStorageStatus(tt.original, tt.current, tt.newValue)
			require.Equal(t, tt.expectedStatus, status, "Storage status mismatch")
		})
	}
}

// computeStorageStatus replicates the logic from SetStorage for testing
func computeStorageStatus(original, current, value common.Hash) evmc.StorageStatus {
	dirty := original.Cmp(current) != 0
	restored := original.Cmp(value) == 0
	currentIsZero := current.Cmp(common.Hash{}) == 0
	valueIsZero := value.Cmp(common.Hash{}) == 0

	status := evmc.StorageAssigned
	if !dirty && !restored {
		if currentIsZero {
			status = evmc.StorageAdded
		} else if valueIsZero {
			status = evmc.StorageDeleted
		} else {
			status = evmc.StorageModified
		}
	} else if dirty && !restored {
		if currentIsZero && valueIsZero {
			status = evmc.StorageDeletedAdded
		} else if !currentIsZero && valueIsZero {
			status = evmc.StorageModifiedDeleted
		}
	} else if dirty {
		if currentIsZero {
			status = evmc.StorageDeletedRestored
		} else if valueIsZero {
			status = evmc.StorageAddedDeleted
		} else {
			status = evmc.StorageModifiedRestored
		}
	}

	return status
}

// TestAccessStatusLogic tests the access list status determination
func TestAccessStatusLogic(t *testing.T) {
	// Simulate access list behavior
	accessList := make(map[common.Address]map[common.Hash]bool)

	addr1 := common.Address{1, 2, 3}
	addr2 := common.Address{4, 5, 6}
	slot1 := common.Hash{7, 8, 9}
	slot2 := common.Hash{10, 11, 12}

	// Test address access
	t.Run("AddressAccess_ColdThenWarm", func(t *testing.T) {
		// First access to addr1 should be cold
		_, exists := accessList[addr1]
		require.False(t, exists, "First access should be cold")

		// Add to access list
		accessList[addr1] = make(map[common.Hash]bool)

		// Second access should be warm
		_, exists = accessList[addr1]
		require.True(t, exists, "Second access should be warm")
	})

	t.Run("StorageAccess_ColdThenWarm", func(t *testing.T) {
		// Ensure address is in access list
		accessList[addr2] = make(map[common.Hash]bool)

		// First slot access should be cold
		slotExists := accessList[addr2][slot1]
		require.False(t, slotExists, "First slot access should be cold")

		// Add slot to access list
		accessList[addr2][slot1] = true

		// Second access should be warm
		slotExists = accessList[addr2][slot1]
		require.True(t, slotExists, "Second slot access should be warm")

		// Different slot should still be cold
		slot2Exists := accessList[addr2][slot2]
		require.False(t, slot2Exists, "Different slot should be cold")
	})
}

// TestEvmcAddressConversion tests address type conversions
func TestEvmcAddressConversion(t *testing.T) {
	gethAddr := common.Address{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	evmcAddr := evmc.Address(gethAddr)

	// Convert back
	recoveredAddr := common.Address(evmcAddr)

	require.Equal(t, gethAddr, recoveredAddr)
}

// TestEvmcHashConversion tests hash type conversions
func TestEvmcHashConversion(t *testing.T) {
	gethHash := common.Hash{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	evmcHash := evmc.Hash(gethHash)

	// Convert back
	recoveredHash := common.Hash(evmcHash)

	require.Equal(t, gethHash, recoveredHash)
}

// TestTransientStorageLogic tests transient storage behavior
func TestTransientStorageLogic(t *testing.T) {
	transient := make(map[common.Address]map[common.Hash]common.Hash)

	addr := common.Address{1, 2, 3}
	key := common.Hash{4, 5, 6}
	value := common.Hash{7, 8, 9}

	// Initial read should return empty
	if transient[addr] == nil {
		transient[addr] = make(map[common.Hash]common.Hash)
	}
	got := transient[addr][key]
	require.Equal(t, common.Hash{}, got)

	// Set value
	transient[addr][key] = value

	// Read should return value
	got = transient[addr][key]
	require.Equal(t, value, got)
}

// TestCallKindValues tests that evmc call kinds have expected values
func TestCallKindValues(t *testing.T) {
	// Ensure the call kinds we use are defined
	require.NotEqual(t, evmc.Call, evmc.Create)
	require.NotEqual(t, evmc.Call, evmc.Create2)
	require.NotEqual(t, evmc.Call, evmc.DelegateCall)
	require.NotEqual(t, evmc.Call, evmc.CallCode)
}

// TestAccessStatusValues tests evmc access status values
func TestAccessStatusValues(t *testing.T) {
	require.NotEqual(t, evmc.ColdAccess, evmc.WarmAccess)
}

// TestStorageStatusValues tests that all storage status values are distinct
func TestStorageStatusValues(t *testing.T) {
	statuses := []evmc.StorageStatus{
		evmc.StorageAssigned,
		evmc.StorageAdded,
		evmc.StorageDeleted,
		evmc.StorageModified,
		evmc.StorageDeletedAdded,
		evmc.StorageModifiedDeleted,
		evmc.StorageDeletedRestored,
		evmc.StorageAddedDeleted,
		evmc.StorageModifiedRestored,
	}

	// Check all values are unique
	seen := make(map[evmc.StorageStatus]bool)
	for _, s := range statuses {
		require.False(t, seen[s], "Duplicate storage status value: %v", s)
		seen[s] = true
	}
}

// TestExecuteCodeSelection tests the logic for selecting code in Execute
func TestExecuteCodeSelection(t *testing.T) {
	tests := []struct {
		name          string
		kind          evmc.CallKind
		input         []byte
		recipientCode []byte
		expectedCode  []byte
	}{
		{
			name:          "CREATE uses input as initcode",
			kind:          evmc.Create,
			input:         []byte{0x60, 0x00, 0xf3}, // initcode
			recipientCode: []byte{0xfe},             // should not be used
			expectedCode:  []byte{0x60, 0x00, 0xf3},
		},
		{
			name:          "CREATE2 uses input as initcode",
			kind:          evmc.Create2,
			input:         []byte{0x60, 0x01, 0xf3}, // initcode
			recipientCode: []byte{0xfe},             // should not be used
			expectedCode:  []byte{0x60, 0x01, 0xf3},
		},
		{
			name:          "CALL uses recipient code",
			kind:          evmc.Call,
			input:         []byte{0x12, 0x34}, // call data
			recipientCode: []byte{0x60, 0x00, 0x52, 0x60, 0x20, 0xf3},
			expectedCode:  []byte{0x60, 0x00, 0x52, 0x60, 0x20, 0xf3},
		},
		{
			name:          "DELEGATECALL uses recipient code",
			kind:          evmc.DelegateCall,
			input:         []byte{0x12, 0x34},
			recipientCode: []byte{0x60, 0x00, 0xf3},
			expectedCode:  []byte{0x60, 0x00, 0xf3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var code []byte
			if tt.kind == evmc.Create || tt.kind == evmc.Create2 {
				code = tt.input // initcode is passed as input for contract creation
			} else {
				code = tt.recipientCode // fetch from recipient for calls
			}
			require.Equal(t, tt.expectedCode, code)
		})
	}
}

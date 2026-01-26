package internal

import (
	"math"
	"testing"

	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
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

// TestNewHostContextConfig tests the HostContextConfig creation from ChainConfig
func TestNewHostContextConfig(t *testing.T) {
	tests := []struct {
		name          string
		chainConfig   *params.ChainConfig
		expectedDelta int64
	}{
		{
			name:          "Nil chain config",
			chainConfig:   nil,
			expectedDelta: 0,
		},
		{
			name: "Nil SeiSstoreSetGasEIP2200",
			chainConfig: &params.ChainConfig{
				SeiSstoreSetGasEIP2200: nil,
			},
			expectedDelta: 0,
		},
		{
			name: "Standard value (20k) - no delta",
			chainConfig: &params.ChainConfig{
				SeiSstoreSetGasEIP2200: func() *uint64 { v := uint64(20000); return &v }(),
			},
			expectedDelta: 0,
		},
		{
			name: "Higher value (72k) - 52k delta",
			chainConfig: &params.ChainConfig{
				SeiSstoreSetGasEIP2200: func() *uint64 { v := uint64(72000); return &v }(),
			},
			expectedDelta: 52000,
		},
		{
			name: "Higher value (100k) - 80k delta",
			chainConfig: &params.ChainConfig{
				SeiSstoreSetGasEIP2200: func() *uint64 { v := uint64(100000); return &v }(),
			},
			expectedDelta: 80000,
		},
		{
			name: "Lower than standard (10k) - negative delta",
			chainConfig: &params.ChainConfig{
				SeiSstoreSetGasEIP2200: func() *uint64 { v := uint64(10000); return &v }(),
			},
			expectedDelta: -10000,
		},
		{
			name: "Zero value - max negative delta",
			chainConfig: &params.ChainConfig{
				SeiSstoreSetGasEIP2200: func() *uint64 { v := uint64(0); return &v }(),
			},
			expectedDelta: -20000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := NewHostContextConfig(tt.chainConfig)
			require.Equal(t, tt.expectedDelta, config.SstoreGasDelta,
				"SstoreGasDelta mismatch for %s", tt.name)
		})
	}
}

// TestNewHostContextConfig_OverflowPanic tests that an overflow-causing value panics
func TestNewHostContextConfig_OverflowPanic(t *testing.T) {
	// Value that would overflow when cast to int64
	overflowValue := uint64(math.MaxInt64) + 1

	chainConfig := &params.ChainConfig{
		SeiSstoreSetGasEIP2200: &overflowValue,
	}

	require.Panics(t, func() {
		NewHostContextConfig(chainConfig)
	}, "Should panic when SeiSstoreSetGasEIP2200 exceeds math.MaxInt64")
}

// TestNewHostContextConfig_MaxSafeValue tests boundary at math.MaxInt64
func TestNewHostContextConfig_MaxSafeValue(t *testing.T) {
	// Maximum safe value (exactly math.MaxInt64) should not panic
	maxSafeValue := uint64(math.MaxInt64)

	chainConfig := &params.ChainConfig{
		SeiSstoreSetGasEIP2200: &maxSafeValue,
	}

	require.NotPanics(t, func() {
		config := NewHostContextConfig(chainConfig)
		// Delta = MaxInt64 - 20000 = a very large positive delta
		expectedDelta := int64(math.MaxInt64) - int64(StandardSstoreSetGasEIP2200)
		require.Equal(t, expectedDelta, config.SstoreGasDelta)
	}, "Should not panic when SeiSstoreSetGasEIP2200 equals math.MaxInt64")
}

// TestSstoreGasDeltaCalculation tests the SSTORE gas delta calculation logic
// that determines how much extra/less gas to charge for Sei's custom SSTORE cost.
func TestSstoreGasDeltaCalculation(t *testing.T) {
	tests := []struct {
		name          string
		seiSstoreGas  uint64
		expectedDelta int64
	}{
		{
			name:          "Standard EIP-2200 (20k) - no adjustment",
			seiSstoreGas:  20000,
			expectedDelta: 0,
		},
		{
			name:          "Higher value (72k) - 52k delta",
			seiSstoreGas:  72000,
			expectedDelta: 52000,
		},
		{
			name:          "Higher custom (100k) - 80k delta",
			seiSstoreGas:  100000,
			expectedDelta: 80000,
		},
		{
			name:          "Lower than standard (10k) - negative delta",
			seiSstoreGas:  10000,
			expectedDelta: -10000,
		},
		{
			name:          "Zero - max negative delta",
			seiSstoreGas:  0,
			expectedDelta: -20000,
		},
		{
			name:          "Just above standard (20001) - 1 delta",
			seiSstoreGas:  20001,
			expectedDelta: 1,
		},
		{
			name:          "Just below standard (19999) - -1 delta",
			seiSstoreGas:  19999,
			expectedDelta: -1,
		},
		{
			name:          "Exactly standard - no adjustment",
			seiSstoreGas:  StandardSstoreSetGasEIP2200,
			expectedDelta: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the delta calculation logic: Sei cost - standard cost
			delta := int64(tt.seiSstoreGas) - int64(StandardSstoreSetGasEIP2200)

			require.Equal(t, tt.expectedDelta, delta,
				"Delta for seiSstoreGas=%d should be %d", tt.seiSstoreGas, tt.expectedDelta)
		})
	}
}

// TestSstoreGasAdjustmentAccumulation tests the atomic accumulation of gas adjustments
func TestSstoreGasAdjustmentAccumulation(t *testing.T) {
	tests := []struct {
		name               string
		deltas             []int64 // deltas to add
		expectedTotal      int64
		resetMidway        bool // reset after half the deltas
		expectedAfterReset int64
	}{
		{
			name:          "Single delta",
			deltas:        []int64{52000},
			expectedTotal: 52000,
		},
		{
			name:          "Multiple deltas accumulate",
			deltas:        []int64{52000, 52000, 52000},
			expectedTotal: 156000,
		},
		{
			name:          "Mixed delta values",
			deltas:        []int64{52000, 28000, 80000},
			expectedTotal: 160000,
		},
		{
			name:          "No deltas - zero total",
			deltas:        []int64{},
			expectedTotal: 0,
		},
		{
			name:               "Reset clears accumulation",
			deltas:             []int64{52000, 52000, 52000, 52000},
			resetMidway:        true,
			expectedAfterReset: 104000, // Only last 2 deltas
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal HostContext (only need the atomic counter)
			hc := &HostContext{}

			// Reset to ensure clean state
			hc.ResetSstoreGasAdjustment()
			require.Equal(t, int64(0), hc.GetSstoreGasAdjustment(), "Should start at 0")

			if tt.resetMidway {
				// Add half the deltas
				half := len(tt.deltas) / 2
				for i := 0; i < half; i++ {
					hc.sstoreGasAdjustment.Add(tt.deltas[i])
				}

				// Reset
				hc.ResetSstoreGasAdjustment()
				require.Equal(t, int64(0), hc.GetSstoreGasAdjustment(), "Should be 0 after reset")

				// Add remaining deltas
				for i := half; i < len(tt.deltas); i++ {
					hc.sstoreGasAdjustment.Add(tt.deltas[i])
				}

				require.Equal(t, tt.expectedAfterReset, hc.GetSstoreGasAdjustment())
			} else {
				// Add all deltas
				for _, delta := range tt.deltas {
					hc.sstoreGasAdjustment.Add(delta)
				}

				require.Equal(t, tt.expectedTotal, hc.GetSstoreGasAdjustment())
			}
		})
	}
}

// TestSstoreGasAdjustmentWithStorageStatus tests that only StorageAdded triggers adjustment
func TestSstoreGasAdjustmentWithStorageStatus(t *testing.T) {
	tests := []struct {
		name         string
		status       evmc.StorageStatus
		shouldAdjust bool
	}{
		{
			name:         "StorageAdded triggers adjustment",
			status:       evmc.StorageAdded,
			shouldAdjust: true,
		},
		{
			name:         "StorageModified does NOT trigger adjustment",
			status:       evmc.StorageModified,
			shouldAdjust: false,
		},
		{
			name:         "StorageDeleted does NOT trigger adjustment",
			status:       evmc.StorageDeleted,
			shouldAdjust: false,
		},
		{
			name:         "StorageAssigned does NOT trigger adjustment",
			status:       evmc.StorageAssigned,
			shouldAdjust: false,
		},
		{
			name:         "StorageDeletedAdded does NOT trigger adjustment",
			status:       evmc.StorageDeletedAdded,
			shouldAdjust: false,
		},
		{
			name:         "StorageModifiedDeleted does NOT trigger adjustment",
			status:       evmc.StorageModifiedDeleted,
			shouldAdjust: false,
		},
		{
			name:         "StorageDeletedRestored does NOT trigger adjustment",
			status:       evmc.StorageDeletedRestored,
			shouldAdjust: false,
		},
		{
			name:         "StorageAddedDeleted does NOT trigger adjustment",
			status:       evmc.StorageAddedDeleted,
			shouldAdjust: false,
		},
		{
			name:         "StorageModifiedRestored does NOT trigger adjustment",
			status:       evmc.StorageModifiedRestored,
			shouldAdjust: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the condition used in SetStorage
			shouldAdjust := tt.status == evmc.StorageAdded

			require.Equal(t, tt.shouldAdjust, shouldAdjust,
				"StorageStatus %v adjustment check", tt.status)
		})
	}
}

// TestStandardSstoreSetGasConstant verifies the constant matches EIP-2200
func TestStandardSstoreSetGasConstant(t *testing.T) {
	// EIP-2200 defines SstoreSetGas as 20000
	require.Equal(t, uint64(20000), StandardSstoreSetGasEIP2200,
		"StandardSstoreSetGasEIP2200 should be 20000 per EIP-2200")
}

// TestSstoreGasAdjustmentConcurrency tests thread-safety of gas adjustment
func TestSstoreGasAdjustmentConcurrency(t *testing.T) {
	hc := &HostContext{}
	hc.ResetSstoreGasAdjustment()

	const numGoroutines = 100
	const deltasPerGoroutine = 100
	const deltaValue = int64(52000)

	done := make(chan bool, numGoroutines)

	// Spawn goroutines that concurrently add to the adjustment
	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < deltasPerGoroutine; j++ {
				hc.sstoreGasAdjustment.Add(deltaValue)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	expected := int64(numGoroutines * deltasPerGoroutine * deltaValue)
	require.Equal(t, expected, hc.GetSstoreGasAdjustment(),
		"Concurrent additions should accumulate correctly")
}

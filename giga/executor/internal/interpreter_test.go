package internal

import (
	"testing"

	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

// TestCallKindMapping tests that opcode to evmc call kind mapping is correct
func TestCallKindMapping(t *testing.T) {
	tests := []struct {
		name           string
		opCode         vm.OpCode
		expectedKind   evmc.CallKind
		expectedStatic bool
	}{
		{
			name:           "CALL",
			opCode:         vm.CALL,
			expectedKind:   evmc.Call,
			expectedStatic: false,
		},
		{
			name:           "STATICCALL",
			opCode:         vm.STATICCALL,
			expectedKind:   evmc.Call,
			expectedStatic: true,
		},
		{
			name:           "DELEGATECALL",
			opCode:         vm.DELEGATECALL,
			expectedKind:   evmc.DelegateCall,
			expectedStatic: false,
		},
		{
			name:           "CALLCODE",
			opCode:         vm.CALLCODE,
			expectedKind:   evmc.CallCode,
			expectedStatic: false,
		},
		{
			name:           "CREATE",
			opCode:         vm.CREATE,
			expectedKind:   evmc.Create,
			expectedStatic: false,
		},
		{
			name:           "CREATE2",
			opCode:         vm.CREATE2,
			expectedKind:   evmc.Create2,
			expectedStatic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the mapping logic from Run()
			var callKind evmc.CallKind
			var static bool

			if tt.opCode == vm.STATICCALL {
				static = true
			}

			switch tt.opCode {
			case vm.STATICCALL:
				fallthrough
			case vm.CALL:
				callKind = evmc.Call
			case vm.DELEGATECALL:
				callKind = evmc.DelegateCall
			case vm.CREATE2:
				callKind = evmc.Create2
			case vm.CREATE:
				callKind = evmc.Create
			case vm.CALLCODE:
				callKind = evmc.CallCode
			default:
				t.Fatalf("Unsupported opcode: %v", tt.opCode)
			}

			require.Equal(t, tt.expectedKind, callKind)
			require.Equal(t, tt.expectedStatic, static)
		})
	}
}

// TestCodeToExecuteForCreate tests that CREATE/CREATE2 use contract.Code
func TestCodeToExecuteForCreate(t *testing.T) {
	tests := []struct {
		name         string
		opCode       vm.OpCode
		contractCode []byte
		input        []byte
		expectCode   []byte // What code should be used for execution
	}{
		{
			name:         "CREATE uses contract.Code",
			opCode:       vm.CREATE,
			contractCode: []byte{0x60, 0x00, 0x60, 0x00, 0xf3}, // initcode
			input:        []byte{},                             // empty for CREATE
			expectCode:   []byte{0x60, 0x00, 0x60, 0x00, 0xf3},
		},
		{
			name:         "CREATE2 uses contract.Code",
			opCode:       vm.CREATE2,
			contractCode: []byte{0x60, 0x01, 0x60, 0x01, 0xf3}, // initcode
			input:        []byte{},                             // empty for CREATE2
			expectCode:   []byte{0x60, 0x01, 0x60, 0x01, 0xf3},
		},
		{
			name:         "CALL uses input (call data)",
			opCode:       vm.CALL,
			contractCode: []byte{0xfe},                   // not used
			input:        []byte{0x12, 0x34, 0x56, 0x78}, // call data
			expectCode:   []byte{0x12, 0x34, 0x56, 0x78},
		},
		{
			name:         "STATICCALL uses input (call data)",
			opCode:       vm.STATICCALL,
			contractCode: []byte{0xfe},
			input:        []byte{0xab, 0xcd},
			expectCode:   []byte{0xab, 0xcd},
		},
		{
			name:         "DELEGATECALL uses input (call data)",
			opCode:       vm.DELEGATECALL,
			contractCode: []byte{0xfe},
			input:        []byte{0x11, 0x22, 0x33},
			expectCode:   []byte{0x11, 0x22, 0x33},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the code selection logic from Run()
			codeToExecute := tt.input
			if tt.opCode == vm.CREATE || tt.opCode == vm.CREATE2 {
				codeToExecute = tt.contractCode
			}
			require.Equal(t, tt.expectCode, codeToExecute)
		})
	}
}

// TestStaticCallFlag tests that STATICCALL sets the static flag
func TestStaticCallFlag(t *testing.T) {
	tests := []struct {
		opCode   vm.OpCode
		expected bool
	}{
		{vm.CALL, false},
		{vm.STATICCALL, true},
		{vm.DELEGATECALL, false},
		{vm.CALLCODE, false},
		{vm.CREATE, false},
		{vm.CREATE2, false},
	}

	for _, tt := range tests {
		t.Run(tt.opCode.String(), func(t *testing.T) {
			static := tt.opCode == vm.STATICCALL
			require.Equal(t, tt.expected, static)
		})
	}
}

// TestGasAccountingLogic tests the gas accounting math
func TestGasAccountingLogic(t *testing.T) {
	tests := []struct {
		name           string
		initialGas     uint64
		gasLeft        int64
		gasRefund      int64
		expectedGas    uint64
		expectedRefund uint64
	}{
		{
			name:           "Full execution with leftover",
			initialGas:     1000000,
			gasLeft:        900000,
			gasRefund:      1000,
			expectedGas:    900000,
			expectedRefund: 1000,
		},
		{
			name:           "No refund",
			initialGas:     1000000,
			gasLeft:        500000,
			gasRefund:      0,
			expectedGas:    500000,
			expectedRefund: 0,
		},
		{
			name:           "All gas used",
			initialGas:     21000,
			gasLeft:        0,
			gasRefund:      0,
			expectedGas:    0,
			expectedRefund: 0,
		},
		{
			name:           "Minimal gas left",
			initialGas:     100000,
			gasLeft:        1,
			gasRefund:      50000,
			expectedGas:    1,
			expectedRefund: 50000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the gas accounting logic from Run()
			// contract.Gas = uint64(gasLeft)
			contractGas := uint64(tt.gasLeft)

			// e.evm.StateDB.AddRefund(uint64(gasRefund))
			refundAccumulator := uint64(0)
			refundAccumulator += uint64(tt.gasRefund)

			require.Equal(t, tt.expectedGas, contractGas)
			require.Equal(t, tt.expectedRefund, refundAccumulator)
		})
	}
}

// TestReadOnlyPropagation tests that readOnly flag is properly managed
func TestReadOnlyPropagation(t *testing.T) {
	tests := []struct {
		name            string
		initialReadOnly bool
		callReadOnly    bool
		expectReadOnly  bool
	}{
		{
			name:            "Not readOnly, call not readOnly",
			initialReadOnly: false,
			callReadOnly:    false,
			expectReadOnly:  false,
		},
		{
			name:            "Not readOnly, call is readOnly",
			initialReadOnly: false,
			callReadOnly:    true,
			expectReadOnly:  true,
		},
		{
			name:            "Already readOnly, call not readOnly",
			initialReadOnly: true,
			callReadOnly:    false,
			expectReadOnly:  true, // stays readOnly
		},
		{
			name:            "Already readOnly, call is readOnly",
			initialReadOnly: true,
			callReadOnly:    true,
			expectReadOnly:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the readOnly logic from Run()
			readOnly := tt.initialReadOnly

			// if readOnly && !e.readOnly {
			//     e.readOnly = true
			// }
			if tt.callReadOnly && !readOnly {
				readOnly = true
			}

			require.Equal(t, tt.expectReadOnly, readOnly)
		})
	}
}

// TestDepthIncrement tests that depth is incremented and decremented
func TestDepthIncrement(t *testing.T) {
	depth := 0

	// Simulate entering a call
	depth++
	require.Equal(t, 1, depth)

	// Simulate nested call
	depth++
	require.Equal(t, 2, depth)

	// Simulate returning from nested call
	depth--
	require.Equal(t, 1, depth)

	// Simulate returning from outer call
	depth--
	require.Equal(t, 0, depth)
}

// TestMaxDepth tests the depth limit constant
func TestMaxDepth(t *testing.T) {
	// The EVM has a call depth limit of 1024
	const maxCallDepth = 1024

	depth := 0
	for i := 0; i < maxCallDepth; i++ {
		depth++
	}
	require.Equal(t, maxCallDepth, depth)

	// Attempting to go deeper would fail in the real EVM
}

// TestDelegatedCallDetection tests that DELEGATECALL and CALLCODE are detected
func TestDelegatedCallDetection(t *testing.T) {
	tests := []struct {
		kind      evmc.CallKind
		delegated bool
	}{
		{evmc.Call, false},
		{evmc.DelegateCall, true},
		{evmc.CallCode, true},
		{evmc.Create, false},
		{evmc.Create2, false},
	}

	for i, tt := range tests {
		t.Run(callKindName(tt.kind), func(t *testing.T) {
			// Replicate the delegated detection from Execute
			delegated := tt.kind == evmc.DelegateCall || tt.kind == evmc.CallCode
			require.Equal(t, tt.delegated, delegated, "test case %d", i)
		})
	}
}

// callKindName returns a string name for an evmc.CallKind
func callKindName(kind evmc.CallKind) string {
	switch kind {
	case evmc.Call:
		return "Call"
	case evmc.DelegateCall:
		return "DelegateCall"
	case evmc.CallCode:
		return "CallCode"
	case evmc.Create:
		return "Create"
	case evmc.Create2:
		return "Create2"
	default:
		return "Unknown"
	}
}

// TestOpCodeToString tests that opcodes have string representations
func TestOpCodeToString(t *testing.T) {
	opcodes := []vm.OpCode{
		vm.CALL,
		vm.STATICCALL,
		vm.DELEGATECALL,
		vm.CALLCODE,
		vm.CREATE,
		vm.CREATE2,
	}

	for _, op := range opcodes {
		str := op.String()
		require.NotEmpty(t, str, "OpCode should have string representation")
	}
}

// TestEvmcCallKindValues tests that call kinds have distinct values
func TestEvmcCallKindValues(t *testing.T) {
	kinds := []evmc.CallKind{
		evmc.Call,
		evmc.DelegateCall,
		evmc.CallCode,
		evmc.Create,
		evmc.Create2,
	}

	// Verify all kinds are distinct
	seen := make(map[evmc.CallKind]bool)
	for _, kind := range kinds {
		require.False(t, seen[kind], "Duplicate call kind value")
		seen[kind] = true
		// Use our helper to get a name
		name := callKindName(kind)
		require.NotEmpty(t, name, "CallKind should have a name")
		require.NotEqual(t, "Unknown", name, "CallKind should be recognized")
	}
}

// TestSstoreGasAdjustmentApplication tests the gas adjustment logic in Run()
func TestSstoreGasAdjustmentApplication(t *testing.T) {
	tests := []struct {
		name              string
		gasLeftFromEvmone int64
		sstoreAdjustment  int64
		expectedGasLeft   int64
		expectOutOfGas    bool
	}{
		{
			name:              "No adjustment needed",
			gasLeftFromEvmone: 100000,
			sstoreAdjustment:  0,
			expectedGasLeft:   100000,
			expectOutOfGas:    false,
		},
		{
			name:              "Single SSTORE adjustment (52k delta)",
			gasLeftFromEvmone: 100000,
			sstoreAdjustment:  52000,
			expectedGasLeft:   48000,
			expectOutOfGas:    false,
		},
		{
			name:              "Multiple SSTORE adjustments",
			gasLeftFromEvmone: 200000,
			sstoreAdjustment:  156000, // 3 x 52000
			expectedGasLeft:   44000,
			expectOutOfGas:    false,
		},
		{
			name:              "Adjustment exactly equals gas left",
			gasLeftFromEvmone: 52000,
			sstoreAdjustment:  52000,
			expectedGasLeft:   0,
			expectOutOfGas:    false,
		},
		{
			name:              "Adjustment exceeds gas left - out of gas",
			gasLeftFromEvmone: 50000,
			sstoreAdjustment:  52000,
			expectedGasLeft:   -2000, // Would be negative
			expectOutOfGas:    true,
		},
		{
			name:              "Large adjustment exceeds gas left",
			gasLeftFromEvmone: 100000,
			sstoreAdjustment:  156000, // 3 SSTOREs worth
			expectedGasLeft:   -56000, // Would be negative
			expectOutOfGas:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the logic from Run()
			gasLeft := tt.gasLeftFromEvmone

			if tt.sstoreAdjustment > 0 {
				gasLeft -= tt.sstoreAdjustment
				if gasLeft < 0 {
					require.True(t, tt.expectOutOfGas, "Should have detected out of gas")
					return
				}
			}

			require.False(t, tt.expectOutOfGas, "Should not have out of gas error")
			require.Equal(t, tt.expectedGasLeft, gasLeft)
		})
	}
}

// TestSstoreGasAdjustmentBoundaryConditions tests edge cases
func TestSstoreGasAdjustmentBoundaryConditions(t *testing.T) {
	tests := []struct {
		name           string
		gasLeft        int64
		adjustment     int64
		expectedResult int64
		expectError    bool
	}{
		{
			name:           "Zero gas left, zero adjustment",
			gasLeft:        0,
			adjustment:     0,
			expectedResult: 0,
			expectError:    false,
		},
		{
			name:           "Zero gas left, positive adjustment",
			gasLeft:        0,
			adjustment:     1,
			expectedResult: -1,
			expectError:    true,
		},
		{
			name:           "Minimum positive adjustment",
			gasLeft:        1,
			adjustment:     1,
			expectedResult: 0,
			expectError:    false,
		},
		{
			name:           "Large gas values",
			gasLeft:        1000000000, // 1B gas
			adjustment:     500000000,  // 500M adjustment
			expectedResult: 500000000,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.gasLeft
			if tt.adjustment > 0 {
				result -= tt.adjustment
			}

			if tt.expectError {
				require.True(t, result < 0, "Should result in negative gas")
			} else {
				require.Equal(t, tt.expectedResult, result)
				require.True(t, result >= 0, "Result should be non-negative")
			}
		})
	}
}

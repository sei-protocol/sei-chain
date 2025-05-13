package utils

import (
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

// Test the SplitTransferBalance method
func TestUtils_SplitTransferBalance(t *testing.T) {
	type args struct {
		totalAmount uint64
		expectedLo  uint16
		expectedHi  uint32
	}

	testCases := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Happy Path",
			args: args{
				totalAmount: 0x0000ABCD12345678,
				expectedLo:  0x5678,
				expectedHi:  0xABCD1234,
			},
			wantErr: false,
		},
		{
			name: "Zero expected hi",
			args: args{
				totalAmount: 0x0000000000005678,
				expectedLo:  0x5678,
				expectedHi:  0x00000000,
			},
			wantErr: false,
		},
		{
			name: "Zero expected lo",
			args: args{
				totalAmount: 0x0000123456780000,
				expectedLo:  0x0000,
				expectedHi:  0x12345678,
			},
			wantErr: false,
		},
		{
			name: "All Zeros",
			args: args{
				totalAmount: 0,
				expectedLo:  0,
				expectedHi:  0,
			},
			wantErr: false,
		},
		{
			name: "Max Amounts",
			args: args{
				totalAmount: 1<<48 - 1,
				expectedLo:  math.MaxUint16,
				expectedHi:  math.MaxUint32,
			},
			wantErr: false,
		},
		{
			name: "Transfer amount exceeds 48 bits",
			args: args{
				totalAmount: 0x0001000000000000,
				expectedLo:  0,
				expectedHi:  0,
			},
			wantErr: true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			amountLo, amountHi, err := SplitTransferBalance(tt.args.totalAmount)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.args.expectedLo, amountLo)
				require.Equal(t, tt.args.expectedHi, amountHi)
			}
		})
	}
}

// Test the CombineTransferAmount method
func TestUtils_CombineTransferAmount(t *testing.T) {
	type args struct {
		expectedTotal uint64
		loBits        uint16
		hiBits        uint32
	}

	testCases := []struct {
		name string
		args args
	}{
		{
			name: "Happy Path",
			args: args{
				expectedTotal: 0x0000ABCD12345678,
				loBits:        0x5678,
				hiBits:        0xABCD1234,
			},
		},
		{
			name: "All Zeroes",
			args: args{
				expectedTotal: 0x0000000000000000,
				loBits:        0x0000,
				hiBits:        0x00000000,
			},
		},
		{
			name: "No Lo Bits",
			args: args{
				expectedTotal: 0x0000123456780000,
				loBits:        0x0000,
				hiBits:        0x12345678,
			},
		},
		{
			name: "No Hi Bits",
			args: args{
				expectedTotal: 0x0000000000001234,
				loBits:        0x1234,
				hiBits:        0x00000000,
			},
		},
		{
			name: "Max Amounts",
			args: args{
				expectedTotal: 1<<48 - 1,
				loBits:        math.MaxUint16,
				hiBits:        math.MaxUint32,
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			totalAmount := CombineTransferAmount(tt.args.loBits, tt.args.hiBits)

			require.Equal(t, new(big.Int).SetUint64(tt.args.expectedTotal), totalAmount)
		})
	}
}

// Test the CombinePendingBalances method
func TestUtils_CombinePendingBalances(t *testing.T) {
	type args struct {
		expectedTotal uint64
		loBits        uint64
		hiBits        uint64
	}

	testCases := []struct {
		name string
		args args
	}{
		{
			name: "Happy Path",
			args: args{
				expectedTotal: 0x0000000000011111,
				loBits:        0x0000000000001111,
				hiBits:        0x0000000000000001,
			},
		},
		{
			name: "Overlap",
			args: args{
				expectedTotal: 0x0000111122221111,
				loBits:        0x0000000011111111,
				hiBits:        0x0000000011111111,
			},
		},
		{
			name: "All Zeroes",
			args: args{
				expectedTotal: 0x0000000000000000,
				loBits:        0x0000000000000000,
				hiBits:        0x0000000000000000,
			},
		},
		{
			name: "No Lo Bits",
			args: args{
				expectedTotal: 0x0000123456780000,
				loBits:        0x0000000000000000,
				hiBits:        0x0000000012345678,
			},
		},
		{
			name: "No Hi Bits",
			args: args{
				expectedTotal: 0x0000000000001234,
				loBits:        0x0000000000001234,
				hiBits:        0x0000000000000000,
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			totalAmount := CombinePendingBalances(new(big.Int).SetUint64(tt.args.loBits), new(big.Int).SetUint64(tt.args.hiBits))

			require.Equal(t, new(big.Int).SetUint64(tt.args.expectedTotal), totalAmount)
		})
	}
}

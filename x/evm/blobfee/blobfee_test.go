package blobfee_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/blobfee"
	"github.com/stretchr/testify/require"
)

func TestBlobBaseFeeForNextBlock(t *testing.T) {
	oneWei := big.NewInt(1)

	tests := []struct {
		name          string
		chainConfig   *params.ChainConfig
		blockTime     uint64
		excessBlobGas *uint64
		expectedFee   *big.Int
		expectNonNil  bool
	}{
		{
			name:          "nil config nil excess returns 1 wei",
			chainConfig:   nil,
			blockTime:     0,
			excessBlobGas: nil,
			expectedFee:   oneWei,
			expectNonNil:  true,
		},
		{
			name:          "nil config with excess returns 1 wei",
			chainConfig:   nil,
			blockTime:     1000,
			excessBlobGas: ptrUint64(0),
			expectedFee:   oneWei,
			expectNonNil:  true,
		},
		{
			name:          "with chain config zero time nil excess",
			chainConfig:   params.MainnetChainConfig,
			blockTime:     0,
			excessBlobGas: nil,
			expectedFee:   oneWei,
			expectNonNil:  true,
		},
		{
			name:          "with chain config non-zero time and excess",
			chainConfig:   params.MainnetChainConfig,
			blockTime:     1700000000,
			excessBlobGas: ptrUint64(393216),
			expectedFee:   oneWei,
			expectNonNil:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := blobfee.BlobBaseFeeForNextBlock(tt.chainConfig, tt.blockTime, tt.excessBlobGas)
			if tt.expectNonNil {
				require.NotNil(t, got)
			}
			require.Equal(t, 0, tt.expectedFee.Cmp(got), "fee should equal expected (1 wei)")
			require.True(t, got.Cmp(utils.Big1) == 0, "result should match utils.Big1")
		})
	}
}

func TestBlobBaseFeeForNextBlock_ReturnsCanonicalOneWei(t *testing.T) {
	// Ensure RPC/execution contract: blob base fee is 1 wei in current implementation.
	got := blobfee.BlobBaseFeeForNextBlock(nil, 0, nil)
	require.Equal(t, utils.Big1, got, "should return value equal to utils.Big1")
}

func ptrUint64(u uint64) *uint64 {
	return &u
}

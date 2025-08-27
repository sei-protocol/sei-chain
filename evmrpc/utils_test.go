package evmrpc_test

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/require"
)

func TestCheckVersion(t *testing.T) {
	testApp := app.Setup(false, false, false)
	k := &testApp.EvmKeeper
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(1)
	testApp.Commit(context.Background()) // bump store version to 1
	require.Nil(t, evmrpc.CheckVersion(ctx, k))
	ctx = ctx.WithBlockHeight(2)
	require.NotNil(t, evmrpc.CheckVersion(ctx, k))
}

func TestParallelRunnerPanicRecovery(t *testing.T) {
	r := evmrpc.NewParallelRunner(10, 10)
	r.Queue <- func() {
		panic("should be handled")
	}
	close(r.Queue)
	require.NotPanics(t, r.Done.Wait)
}

func TestValidateBlockAccess(t *testing.T) {
	tests := []struct {
		name        string
		blockNumber int64
		params      evmrpc.BlockValidationParams
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid block within lookback",
			blockNumber: 95,
			params: evmrpc.BlockValidationParams{
				LatestHeight:     100,
				MaxBlockLookback: 10,
				EarliestVersion:  90,
			},
			expectError: false,
		},
		{
			name:        "block beyond max lookback",
			blockNumber: 80,
			params: evmrpc.BlockValidationParams{
				LatestHeight:     100,
				MaxBlockLookback: 10,
				EarliestVersion:  70,
			},
			expectError: true,
			errorMsg:    "beyond max lookback",
		},
		{
			name:        "block before earliest version",
			blockNumber: 50,
			params: evmrpc.BlockValidationParams{
				LatestHeight:     100,
				MaxBlockLookback: 50,
				EarliestVersion:  60,
			},
			expectError: true,
			errorMsg:    "height not available",
		},
		{
			name:        "unlimited lookback (negative value)",
			blockNumber: 1,
			params: evmrpc.BlockValidationParams{
				LatestHeight:     100,
				MaxBlockLookback: -1,
				EarliestVersion:  1,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := evmrpc.ValidateBlockAccess(tt.blockNumber, tt.params)
			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateBlockNumberAccess(t *testing.T) {
	params := evmrpc.BlockValidationParams{
		LatestHeight:     100,
		MaxBlockLookback: 10,
		EarliestVersion:  80,
	}

	tests := []struct {
		name        string
		blockNumber rpc.BlockNumber
		expectError bool
	}{
		{
			name:        "latest block number",
			blockNumber: rpc.LatestBlockNumber,
			expectError: false,
		},
		{
			name:        "finalized block number",
			blockNumber: rpc.FinalizedBlockNumber,
			expectError: false,
		},
		{
			name:        "valid specific block number",
			blockNumber: rpc.BlockNumber(95),
			expectError: false,
		},
		{
			name:        "block number beyond lookback",
			blockNumber: rpc.BlockNumber(80),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := evmrpc.ValidateBlockNumberAccess(tt.blockNumber, params)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateBlockHashAccess(t *testing.T) {
	params := evmrpc.BlockValidationParams{
		LatestHeight:     MockHeight8,
		MaxBlockLookback: 10,
		EarliestVersion:  1,
	}

	// Test with a valid hash that maps to a valid block
	validHash := common.HexToHash(TestBlockHash)
	err := evmrpc.ValidateBlockHashAccess(context.Background(), &MockClient{}, validHash, params)
	require.NoError(t, err)

	// Test with an invalid hash
	invalidHash := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000999")
	err = evmrpc.ValidateBlockHashAccess(context.Background(), &MockBadClient{}, invalidHash, params)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to get block by hash")
}

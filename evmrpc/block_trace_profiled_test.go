package evmrpc

import (
	"context"
	"math/big"
	"testing"

	gethcommon "github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/eth/tracers/tracersutils"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/stretchr/testify/require"
)

func TestShouldUseProfiledBlockTrace(t *testing.T) {
	t.Parallel()

	defaultTracer := ""
	callTracer := "callTracer"

	tests := []struct {
		name     string
		enabled  bool
		config   *tracers.TraceConfig
		expected bool
	}{
		{
			name:     "disabled by default with nil config",
			enabled:  false,
			config:   nil,
			expected: false,
		},
		{
			name:    "disabled by default with default tracer",
			enabled: false,
			config: &tracers.TraceConfig{
				Tracer: &defaultTracer,
			},
			expected: false,
		},
		{
			name:     "enabled with nil config",
			enabled:  true,
			config:   nil,
			expected: true,
		},
		{
			name:    "enabled with default tracer",
			enabled: true,
			config: &tracers.TraceConfig{
				Tracer: &defaultTracer,
			},
			expected: true,
		},
		{
			name:    "explicit tracer keeps legacy path",
			enabled: true,
			config: &tracers.TraceConfig{
				Tracer: &callTracer,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			api := &DebugAPI{profiledBlockTrace: tt.enabled}
			require.Equal(t, tt.expected, api.shouldUseProfiledBlockTrace(tt.config))
		})
	}
}

func testProfiledTraceBlock(t *testing.T) (*gethtypes.Block, gethcommon.Hash, gethtypes.Signer) {
	t.Helper()

	header := &gethtypes.Header{Number: big.NewInt(1)}
	block := gethtypes.NewBlock(header, &gethtypes.Body{}, nil, trie.NewStackTrie(nil))
	blockHash := block.Hash()
	signer := gethtypes.LatestSignerForChainID(big.NewInt(1))
	return block, blockHash, signer
}

func TestProfiledTraceBlockSequentialMetadataLoopRespectsContext(t *testing.T) {
	t.Parallel()

	block, blockHash, signer := testProfiledTraceBlock(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var secondRunnableCalls int
	metadata := []tracersutils.TraceBlockMetadata{
		{
			// Expire the trace context mid-replay; iteration 2 must not run.
			TraceRunnable: func(vm.StateDB) { cancel() },
		},
		{
			TraceRunnable: func(vm.StateDB) { secondRunnableCalls++ },
		},
	}

	api := &DebugAPI{}
	got, err := api.profiledTraceBlockSequential(
		ctx,
		block,
		metadata,
		nil,
		nil,
		vm.BlockContext{},
		signer,
		blockHash,
		make([]*tracers.TxTraceResult, 0),
	)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
	require.Zero(t, secondRunnableCalls)
}

func TestProfiledTraceBlockSequentialEVMOnlyLoopRespectsContext(t *testing.T) {
	t.Parallel()

	block, blockHash, signer := testProfiledTraceBlock(t)
	body := &gethtypes.Body{
		Transactions: gethtypes.Transactions{
			gethtypes.NewTx(&gethtypes.LegacyTx{}),
			gethtypes.NewTx(&gethtypes.LegacyTx{}),
		},
	}
	block = gethtypes.NewBlock(block.Header(), body, nil, trie.NewStackTrie(nil))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	api := &DebugAPI{}
	results := make([]*tracers.TxTraceResult, len(body.Transactions))
	got, err := api.profiledTraceBlockSequential(
		ctx,
		block,
		nil,
		nil,
		nil,
		vm.BlockContext{},
		signer,
		blockHash,
		results,
	)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
}

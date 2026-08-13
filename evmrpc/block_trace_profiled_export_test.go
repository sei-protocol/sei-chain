package evmrpc_test

import (
	"context"
	"math/big"
	"testing"

	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers/tracersutils"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/sei-protocol/sei-chain/evmrpc"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestProfiledTraceBlockParallelMetadataLoopRespectsContext(t *testing.T) {
	t.Parallel()

	header := &gethtypes.Header{Number: big.NewInt(1)}
	block := gethtypes.NewBlock(header, &gethtypes.Body{}, nil, trie.NewStackTrie(nil))
	blockHash := block.Hash()
	signer := gethtypes.LatestSignerForChainID(big.NewInt(1))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var secondRunnableCalls int
	metadata := []tracersutils.TraceBlockMetadata{
		{
			TraceRunnable: func(vm.StateDB) { cancel() },
		},
		{
			TraceRunnable: func(vm.StateDB) { secondRunnableCalls++ },
		},
	}

	stateDB := state.NewDBImpl(Ctx, EVMKeeper, false)
	api := evmrpc.NewDebugAPIForTest(evmrpc.NewTraceBackendForTest(EVMKeeper, func(int64) sdk.Context { return Ctx }))
	got, err := evmrpc.ProfiledTraceBlockParallelForTest(
		api,
		ctx,
		block,
		metadata,
		nil,
		stateDB,
		signer,
		blockHash,
		nil,
		2,
	)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
	require.Zero(t, secondRunnableCalls)
}

package evmrpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app/legacyabci"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestReleaseOnContextPanic(t *testing.T) {
	t.Parallel()

	var released int
	release := func() { released++ }

	err := releaseOnContextPanic(release, context.Canceled)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, released)

	err = releaseOnContextPanic(release, context.DeadlineExceeded)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 2, released)

	err = releaseOnContextPanic(release, fmt.Errorf("skip: %w", context.DeadlineExceeded))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 3, released)

	require.Panics(t, func() {
		_ = releaseOnContextPanic(release, errors.New("pebble seek failed"))
	})
	require.Equal(t, 4, released)

	require.Panics(t, func() {
		_ = releaseOnContextPanic(release, "not an error")
	})
	require.Equal(t, 5, released)
}

func TestInitializeBlockReleasesLeaseOnBeginBlockDeadline(t *testing.T) {
	orig := runTraceBeginBlock
	t.Cleanup(func() { runTraceBeginBlock = orig })
	runTraceBeginBlock = func(sdk.Context, int64, []abci.VoteInfo, []abci.Misbehavior, legacyabci.BeginBlockKeepers) {
		panic(context.DeadlineExceeded)
	}

	var released int
	backend, block := newInitializeBlockTestBackend(t)
	_, _, release, err := backend.initializeBlock(t.Context(), block, func(int64) (sdk.Context, func()) {
		return sdk.Context{}, func() { released++ }
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 1, released, "base snapshot lease must be released on BeginBlock abort")
	release()
	require.Equal(t, 1, released, "returned release must be a no-op after recover")
}

func TestInitializeBlockReleasesLeaseOnUnrelatedBeginBlockPanic(t *testing.T) {
	orig := runTraceBeginBlock
	t.Cleanup(func() { runTraceBeginBlock = orig })
	runTraceBeginBlock = func(sdk.Context, int64, []abci.VoteInfo, []abci.Misbehavior, legacyabci.BeginBlockKeepers) {
		panic("boom")
	}

	var released int
	backend, block := newInitializeBlockTestBackend(t)
	require.Panics(t, func() {
		_, _, _, _ = backend.initializeBlock(t.Context(), block, func(int64) (sdk.Context, func()) {
			return sdk.Context{}, func() { released++ }
		})
	})
	require.Equal(t, 1, released)
}

func newInitializeBlockTestBackend(t *testing.T) (*Backend, *ethtypes.Block) {
	t.Helper()
	tm := &fakeTMClient{
		status: &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: 10, EarliestBlockHeight: 1}},
		blocksByHeight: map[int64]*coretypes.ResultBlock{
			8: {
				Block: &tmtypes.Block{
					Header:     tmtypes.Header{Height: 8},
					LastCommit: &tmtypes.Commit{},
				},
			},
		},
	}
	return &Backend{
		tmClient:   tm,
		watermarks: newTestWatermarkManager(tm, 10, nil, 10),
	}, ethtypes.NewBlock(
		&ethtypes.Header{Number: big.NewInt(8), Time: 1, Difficulty: big.NewInt(0)},
		&ethtypes.Body{},
		nil,
		trie.NewStackTrie(nil),
	)
}

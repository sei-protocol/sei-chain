package evmrpc

import (
	"context"
	"encoding/hex"
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
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func mustHexToBytes(h string) []byte {
	bz, err := hex.DecodeString(h)
	if err != nil {
		panic(err)
	}
	return bz
}

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

func TestInitializeBlockUsesTracedBlockAppHash(t *testing.T) {
	orig := runTraceBeginBlock
	t.Cleanup(func() { runTraceBeginBlock = orig })
	runTraceBeginBlock = func(sdk.Context, int64, []abci.VoteInfo, []abci.Misbehavior, legacyabci.BeginBlockKeepers) {
	}

	tracedAppHash := bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000008"))
	latestAppHash := bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000010"))

	backend, block := newInitializeBlockTestBackend(t)
	backend.tmClient.(*fakeTMClient).blocksByHeight[8].Block.AppHash = tracedAppHash

	// The base ctx's latest-head AppHash must not leak into the trace ctx.
	baseCtx := sdk.Context{}.WithBlockHeader(tmproto.Header{AppHash: latestAppHash})
	sdkCtx, _, release, err := backend.initializeBlock(t.Context(), block, func(int64) (sdk.Context, func()) {
		return baseCtx, func() {}
	})
	require.NoError(t, err)
	defer release()
	require.Equal(t, []byte(tracedAppHash), []byte(sdkCtx.BlockHeader().AppHash))
}

// TestInitializeBlockGigaHeightTracesWithHeadAppHash pins the Autobahn
// limitation documented at the guard in initializeBlock: translateGlobalBlock
// leaves AppHash empty, so a historical giga height traces with head's app
// hash rather than the one it executed with. Zeroing it instead would be
// worse — PREVRANDAO would read 0x00..00 — but neither value is faithful.
// Recovering the executed hash means reading the commit hash at height-1.
func TestInitializeBlockGigaHeightTracesWithHeadAppHash(t *testing.T) {
	orig := runTraceBeginBlock
	t.Cleanup(func() { runTraceBeginBlock = orig })
	runTraceBeginBlock = func(sdk.Context, int64, []abci.VoteInfo, []abci.Misbehavior, legacyabci.BeginBlockKeepers) {
	}

	headAppHash := bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000010"))

	backend, block := newInitializeBlockTestBackend(t)
	// Autobahn's translateGlobalBlock populates only ChainID/Height/Time.
	require.Empty(t, backend.tmClient.(*fakeTMClient).blocksByHeight[8].Block.AppHash)

	// The base ctx is opened at the traced height but keeps the check (head)
	// header, so its AppHash is head's — see App.RPCContextProvider.
	baseCtx := sdk.Context{}.WithBlockHeader(tmproto.Header{Height: 5000, AppHash: headAppHash})
	sdkCtx, _, release, err := backend.initializeBlock(t.Context(), block, func(int64) (sdk.Context, func()) {
		return baseCtx, func() {}
	})
	require.NoError(t, err)
	defer release()
	require.Equal(t, int64(8), sdkCtx.BlockHeight())
	require.Equal(t, []byte(headAppHash), []byte(sdkCtx.BlockHeader().AppHash),
		"documented limitation: giga traces carry head's app hash, not height 8's")
	require.NotEmpty(t, sdkCtx.BlockHeader().AppHash, "must not zero PREVRANDAO")
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

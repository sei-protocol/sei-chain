package statesync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/statesync/mocks"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	ssproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/statesync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
)

func TestSyncer_SyncAny(t *testing.T) {
	ctx := t.Context()

	state := sm.State{
		ChainID: "chain",
		Version: sm.Version{
			Consensus: version.Consensus{
				Block: version.BlockProtocol,
				App:   testAppVersion,
			},
			Software: version.TMVersion,
		},

		LastBlockHeight: 1,
		LastBlockID:     types.BlockID{Hash: []byte("blockhash")},
		LastBlockTime:   time.Now(),
		LastResultsHash: []byte("last_results_hash"),
		AppHash:         []byte("app_hash"),

		LastValidators: &types.ValidatorSet{Proposer: &types.Validator{Address: []byte("val1")}},
		Validators:     &types.ValidatorSet{Proposer: &types.Validator{Address: []byte("val2")}},
		NextValidators: &types.ValidatorSet{Proposer: &types.Validator{Address: []byte("val3")}},

		ConsensusParams:                  *types.DefaultConsensusParams(),
		LastHeightConsensusParamsChanged: 1,
	}
	commit := &types.Commit{BlockID: types.BlockID{Hash: []byte("blockhash")}}

	chunks := []*chunk{
		{Height: 1, Format: 1, Index: 0, Chunk: []byte{1, 1, 0}},
		{Height: 1, Format: 1, Index: 1, Chunk: []byte{1, 1, 1}},
		{Height: 1, Format: 1, Index: 2, Chunk: []byte{1, 1, 2}},
	}
	s := &snapshot{Height: 1, Format: 1, Chunks: 3, Hash: []byte{1, 2, 3}}

	stateProvider := &mocks.StateProvider{}
	stateProvider.On("AppHash", mock.Anything, uint64(1)).Return(state.AppHash, nil)
	stateProvider.On("AppHash", mock.Anything, uint64(2)).Return([]byte("app_hash_2"), nil)
	stateProvider.On("Commit", mock.Anything, uint64(1)).Return(commit, nil)
	stateProvider.On("State", mock.Anything, uint64(1)).Return(state, nil)
	app := newTestStatesyncApp()

	rejectReq := &abci.RequestOfferSnapshot{
		Snapshot: &abci.Snapshot{
			Height: 2,
			Format: 2,
			Chunks: 3,
			Hash:   []byte{1},
		},
		AppHash: []byte("app_hash_2"),
	}
	app.offerSnapshot.Push(func(_ context.Context, req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
		require.Equal(t, rejectReq, req)
		return &abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_REJECT_FORMAT}, nil
	})
	acceptReq := &abci.RequestOfferSnapshot{
		Snapshot: &abci.Snapshot{
			Height:   s.Height,
			Format:   s.Format,
			Chunks:   s.Chunks,
			Hash:     s.Hash,
			Metadata: s.Metadata,
		},
		AppHash: []byte("app_hash"),
	}
	for range 2 {
		app.offerSnapshot.Push(func(_ context.Context, req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
			require.Equal(t, acceptReq, req)
			return &abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_ACCEPT}, nil
		})
	}

	rts := setup(t, app, stateProvider, true)
	peerA := rts.AddPeer(t)
	peerB := rts.AddPeer(t)
	peerC := rts.AddPeer(t)

	// Adding a chunk should error when no sync is in progress
	_, err := rts.reactor.syncer.AddChunk(&chunk{Height: 1, Format: 1, Index: 0, Chunk: []byte{1}})
	require.Error(t, err)

	// Adding a couple of peers should trigger snapshot discovery messages
	rts.reactor.syncer.AddPeer(peerA.NodeID)
	m, err := peerA.snapshotCh.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, wrap(&ssproto.SnapshotsRequest{}), m.Message)

	rts.reactor.syncer.AddPeer(peerB.NodeID)
	m, err = peerB.snapshotCh.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, wrap(&ssproto.SnapshotsRequest{}), m.Message)

	// Both peers report back with snapshots. One of them also returns a snapshot we don't want, in
	// format 2, which will be rejected by the ABCI application.
	new, err := rts.reactor.syncer.AddSnapshot(peerA.NodeID, s)
	require.NoError(t, err)
	require.True(t, new)

	new, err = rts.reactor.syncer.AddSnapshot(peerB.NodeID, s)
	require.NoError(t, err)
	require.False(t, new)

	s2 := &snapshot{Height: 2, Format: 2, Chunks: 3, Hash: []byte{1}}
	new, err = rts.reactor.syncer.AddSnapshot(peerB.NodeID, s2)
	require.NoError(t, err)
	require.True(t, new)

	new, err = rts.reactor.syncer.AddSnapshot(peerC.NodeID, s2)
	require.NoError(t, err)
	require.False(t, new)

	// We start a sync, with peers sending back chunks when requested. We first reject the snapshot
	// with height 2 format 2, and accept the snapshot at height 1.
	chunkRequests := map[uint32]int{}
	chunkRequestsMtx := sync.Mutex{}
	var seen int
	err = scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for _, peer := range utils.Slice(peerA, peerB, peerC) {
			s.SpawnBg(func() error {
				for {
					m, err := peer.chunkCh.Recv(ctx)
					if err != nil {
						return nil
					}
					req := m.Message.Sum.(*ssproto.Message_ChunkRequest).ChunkRequest
					if req.Height != 1 {
						return fmt.Errorf("expected height 1, got %d", req.Height)
					}
					if req.Format != 1 {
						return fmt.Errorf("expected format 1, got %d", req.Format)
					}
					if req.Index >= uint32(len(chunks)) {
						return fmt.Errorf("requested index out of range: %d", req.Index)
					}
					added, err := rts.reactor.syncer.AddChunk(chunks[req.Index])
					if err != nil {
						return fmt.Errorf("AddChunk(): %w", err)
					}
					if !added {
						return fmt.Errorf("chunk %d was not added", req.Index)
					}
					chunkRequestsMtx.Lock()
					chunkRequests[req.Index]++
					seen++
					chunkRequestsMtx.Unlock()
				}
			})
		}

		app.applySnapshotChunk.Push(mkHandler(
			&abci.RequestApplySnapshotChunk{Index: 0, Chunk: []byte{1, 1, 0}},
			&abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ACCEPT},
		))
		app.applySnapshotChunk.Push(mkHandler(
			&abci.RequestApplySnapshotChunk{Index: 1, Chunk: []byte{1, 1, 1}},
			&abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ACCEPT},
		))
		// The first time we're applying chunk 2 we tell it to retry the snapshot and discard chunk 1,
		// which should cause it to keep the existing chunk 0 and 2, and restart restoration from
		// beginning. We also wait for a little while, to exercise the retry logic in fetchChunks().
		app.applySnapshotChunk.Push(func(ctx context.Context, req *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
			require.Equal(t, req, &abci.RequestApplySnapshotChunk{
				Index: 2, Chunk: []byte{1, 1, 2},
			})
			if err := utils.Sleep(ctx, time.Second); err != nil {
				return nil, err
			}
			return &abci.ResponseApplySnapshotChunk{
				Result:        abci.ResponseApplySnapshotChunk_RETRY_SNAPSHOT,
				RefetchChunks: []uint32{1},
			}, nil
		})
		app.applySnapshotChunk.Push(mkHandler(
			&abci.RequestApplySnapshotChunk{Index: 0, Chunk: []byte{1, 1, 0}},
			&abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ACCEPT},
		))
		app.applySnapshotChunk.Push(mkHandler(
			&abci.RequestApplySnapshotChunk{Index: 1, Chunk: []byte{1, 1, 1}},
			&abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ACCEPT},
		))
		app.applySnapshotChunk.Push(mkHandler(
			&abci.RequestApplySnapshotChunk{Index: 2, Chunk: []byte{1, 1, 2}},
			&abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ACCEPT},
		))
		app.info.Push(mkHandler(&proxy.RequestInfo, &abci.ResponseInfo{
			AppVersion:       testAppVersion,
			LastBlockHeight:  1,
			LastBlockAppHash: []byte("app_hash"),
		}))

		newState, lastCommit, err := rts.reactor.syncer.SyncAny(ctx, 0, func() error { return nil })
		if err != nil {
			return fmt.Errorf("rts.reactor.syncer.SyncAny(): %w", err)
		}
		// TODO(gprusak): we should have used utils.TestDiff here, however ValidatorSet has dynamic unexported field and
		// does NOT provide Equal method.
		if ok := assert.Equal(t, state, newState); !ok {
			return fmt.Errorf("state mismatch, got %v want %v", newState, state)
		}
		if ok := assert.Equal(t, commit, lastCommit); !ok {
			return fmt.Errorf("commit mismatch, got %v want %v", lastCommit, commit)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	chunkRequestsMtx.Lock()
	require.Equal(t, map[uint32]int{0: 1, 1: 2, 2: 1}, chunkRequests)
	chunkRequestsMtx.Unlock()

	require.Equal(t, len(chunks), int(rts.reactor.syncer.processingSnapshot.Chunks))
	require.Equal(t, state.LastBlockHeight, rts.reactor.syncer.lastSyncedSnapshotHeight)
	require.True(t, rts.reactor.syncer.avgChunkTime > 0)

	require.Equal(t, int64(rts.reactor.syncer.processingSnapshot.Chunks), rts.reactor.SnapshotChunksTotal())
	require.Equal(t, rts.reactor.syncer.lastSyncedSnapshotHeight, rts.reactor.SnapshotHeight())
	require.Equal(t, time.Duration(rts.reactor.syncer.avgChunkTime), rts.reactor.ChunkProcessAvgTime())
	require.Equal(t, int64(len(rts.reactor.syncer.snapshots.snapshots)), rts.reactor.TotalSnapshots())
	require.Equal(t, int64(0), rts.reactor.SnapshotChunksCount())

	app.AssertExpectations(t)
}

func TestSyncer_SyncAny_noSnapshots(t *testing.T) {
	stateProvider := &mocks.StateProvider{}
	stateProvider.On("AppHash", mock.Anything, mock.Anything).Return([]byte("app_hash"), nil)

	ctx := t.Context()

	rts := setup(t, nil, stateProvider, true)

	_, _, err := rts.reactor.syncer.SyncAny(ctx, 0, func() error { return nil })
	require.Equal(t, errNoSnapshots, err)
}

func TestSyncer_SyncAny_abort(t *testing.T) {
	stateProvider := &mocks.StateProvider{}
	stateProvider.On("AppHash", mock.Anything, mock.Anything).Return([]byte("app_hash"), nil)

	ctx := t.Context()

	rts := setup(t, nil, stateProvider, true)
	app := rts.conn

	s := &snapshot{Height: 1, Format: 1, Chunks: 3, Hash: []byte{1, 2, 3}}
	peerID := types.NodeID("aa")

	_, err := rts.reactor.syncer.AddSnapshot(peerID, s)
	require.NoError(t, err)

	app.offerSnapshot.Push(mkHandler(
		&abci.RequestOfferSnapshot{Snapshot: toABCI(s), AppHash: []byte("app_hash")},
		&abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_ABORT},
	))

	_, _, err = rts.reactor.syncer.SyncAny(ctx, 0, func() error { return nil })
	require.Equal(t, errAbort, err)
	app.AssertExpectations(t)
}

func TestSyncer_SyncAny_reject(t *testing.T) {
	stateProvider := &mocks.StateProvider{}
	stateProvider.On("AppHash", mock.Anything, mock.Anything).Return([]byte("app_hash"), nil)

	ctx := t.Context()

	rts := setup(t, nil, stateProvider, true)
	app := rts.conn

	// s22 is tried first, then s12, then s11, then errNoSnapshots
	s22 := &snapshot{Height: 2, Format: 2, Chunks: 3, Hash: []byte{1, 2, 3}}
	s12 := &snapshot{Height: 1, Format: 2, Chunks: 3, Hash: []byte{1, 2, 3}}
	s11 := &snapshot{Height: 1, Format: 1, Chunks: 3, Hash: []byte{1, 2, 3}}

	peerID := types.NodeID("aa")

	_, err := rts.reactor.syncer.AddSnapshot(peerID, s22)
	require.NoError(t, err)

	_, err = rts.reactor.syncer.AddSnapshot(peerID, s12)
	require.NoError(t, err)

	_, err = rts.reactor.syncer.AddSnapshot(peerID, s11)
	require.NoError(t, err)

	app.offerSnapshot.Push(mkHandler(
		&abci.RequestOfferSnapshot{Snapshot: toABCI(s22), AppHash: []byte("app_hash")},
		&abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_REJECT},
	))
	app.offerSnapshot.Push(mkHandler(
		&abci.RequestOfferSnapshot{Snapshot: toABCI(s12), AppHash: []byte("app_hash")},
		&abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_REJECT},
	))
	app.offerSnapshot.Push(mkHandler(
		&abci.RequestOfferSnapshot{Snapshot: toABCI(s11), AppHash: []byte("app_hash")},
		&abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_REJECT},
	))

	_, _, err = rts.reactor.syncer.SyncAny(ctx, 0, func() error { return nil })
	require.Equal(t, errNoSnapshots, err)
	app.AssertExpectations(t)
}

func TestSyncer_SyncAny_reject_format(t *testing.T) {
	stateProvider := &mocks.StateProvider{}
	stateProvider.On("AppHash", mock.Anything, mock.Anything).Return([]byte("app_hash"), nil)

	ctx := t.Context()

	rts := setup(t, nil, stateProvider, true)
	app := rts.conn

	// s22 is tried first, which reject s22 and s12, then s11 will abort.
	s22 := &snapshot{Height: 2, Format: 2, Chunks: 3, Hash: []byte{1, 2, 3}}
	s12 := &snapshot{Height: 1, Format: 2, Chunks: 3, Hash: []byte{1, 2, 3}}
	s11 := &snapshot{Height: 1, Format: 1, Chunks: 3, Hash: []byte{1, 2, 3}}

	peerID := types.NodeID("aa")

	_, err := rts.reactor.syncer.AddSnapshot(peerID, s22)
	require.NoError(t, err)

	_, err = rts.reactor.syncer.AddSnapshot(peerID, s12)
	require.NoError(t, err)

	_, err = rts.reactor.syncer.AddSnapshot(peerID, s11)
	require.NoError(t, err)

	app.offerSnapshot.Push(mkHandler(
		&abci.RequestOfferSnapshot{Snapshot: toABCI(s22), AppHash: []byte("app_hash")},
		&abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_REJECT_FORMAT},
	))
	app.offerSnapshot.Push(mkHandler(
		&abci.RequestOfferSnapshot{Snapshot: toABCI(s11), AppHash: []byte("app_hash")},
		&abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_ABORT},
	))

	_, _, err = rts.reactor.syncer.SyncAny(ctx, 0, func() error { return nil })
	require.Equal(t, errAbort, err)
	app.AssertExpectations(t)
}

func TestSyncer_SyncAny_reject_sender(t *testing.T) {
	stateProvider := &mocks.StateProvider{}
	stateProvider.On("AppHash", mock.Anything, mock.Anything).Return([]byte("app_hash"), nil)

	ctx := t.Context()

	rts := setup(t, nil, stateProvider, true)
	app := rts.conn

	peerA := rts.AddPeer(t)
	peerB := rts.AddPeer(t)
	peerC := rts.AddPeer(t)

	// sbc will be offered first, which will be rejected with reject_sender, causing all snapshots
	// submitted by both b and c (i.e. sb, sc, sbc) to be rejected. Finally, sa will reject and
	// errNoSnapshots is returned.
	sa := &snapshot{Height: 1, Format: 1, Chunks: 3, Hash: []byte{1, 2, 3}}
	sb := &snapshot{Height: 2, Format: 1, Chunks: 3, Hash: []byte{1, 2, 3}}
	sc := &snapshot{Height: 3, Format: 1, Chunks: 3, Hash: []byte{1, 2, 3}}
	sbc := &snapshot{Height: 4, Format: 1, Chunks: 3, Hash: []byte{1, 2, 3}}

	_, err := rts.reactor.syncer.AddSnapshot(peerA.NodeID, sa)
	require.NoError(t, err)

	_, err = rts.reactor.syncer.AddSnapshot(peerB.NodeID, sb)
	require.NoError(t, err)

	_, err = rts.reactor.syncer.AddSnapshot(peerC.NodeID, sc)
	require.NoError(t, err)

	_, err = rts.reactor.syncer.AddSnapshot(peerB.NodeID, sbc)
	require.NoError(t, err)

	_, err = rts.reactor.syncer.AddSnapshot(peerC.NodeID, sbc)
	require.NoError(t, err)

	app.offerSnapshot.Push(mkHandler(
		&abci.RequestOfferSnapshot{Snapshot: toABCI(sbc), AppHash: []byte("app_hash")},
		&abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_REJECT_SENDER},
	))
	app.offerSnapshot.Push(mkHandler(
		&abci.RequestOfferSnapshot{Snapshot: toABCI(sa), AppHash: []byte("app_hash")},
		&abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_REJECT},
	))

	_, _, err = rts.reactor.syncer.SyncAny(ctx, 0, func() error { return nil })
	require.Equal(t, errNoSnapshots, err)
	app.AssertExpectations(t)
}

func TestSyncer_SyncAny_abciError(t *testing.T) {
	stateProvider := &mocks.StateProvider{}
	stateProvider.On("AppHash", mock.Anything, mock.Anything).Return([]byte("app_hash"), nil)

	ctx := t.Context()

	rts := setup(t, nil, stateProvider, true)
	app := rts.conn

	errBoom := errors.New("boom")
	s := &snapshot{Height: 1, Format: 1, Chunks: 3, Hash: []byte{1, 2, 3}}

	peerID := types.NodeID("aa")

	_, err := rts.reactor.syncer.AddSnapshot(peerID, s)
	require.NoError(t, err)

	app.offerSnapshot.Push(func(_ context.Context, req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
		want := &abci.RequestOfferSnapshot{Snapshot: toABCI(s), AppHash: []byte("app_hash")}
		utils.OrPanic(utils.TestDiff(want, req))
		return nil, errBoom
	})

	_, _, err = rts.reactor.syncer.SyncAny(ctx, 0, func() error { return nil })
	require.True(t, errors.Is(err, errBoom))
	app.AssertExpectations(t)
}

func TestSyncer_offerSnapshot(t *testing.T) {
	unknownErr := errors.New("unknown error")
	boom := errors.New("boom")

	testcases := map[string]struct {
		result    abci.ResponseOfferSnapshot_Result
		err       error
		expectErr error
	}{
		"accept":           {abci.ResponseOfferSnapshot_ACCEPT, nil, nil},
		"abort":            {abci.ResponseOfferSnapshot_ABORT, nil, errAbort},
		"reject":           {abci.ResponseOfferSnapshot_REJECT, nil, errRejectSnapshot},
		"reject_format":    {abci.ResponseOfferSnapshot_REJECT_FORMAT, nil, errRejectFormat},
		"reject_sender":    {abci.ResponseOfferSnapshot_REJECT_SENDER, nil, errRejectSender},
		"unknown":          {abci.ResponseOfferSnapshot_UNKNOWN, nil, unknownErr},
		"error":            {0, boom, boom},
		"unknown non-zero": {9, nil, unknownErr},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()

			stateProvider := &mocks.StateProvider{}
			stateProvider.On("AppHash", mock.Anything, mock.Anything).Return([]byte("app_hash"), nil)

			rts := setup(t, nil, stateProvider, true)
			app := rts.conn
			s := &snapshot{Height: 1, Format: 1, Chunks: 3, Hash: []byte{1, 2, 3}, trustedAppHash: []byte("app_hash")}
			app.offerSnapshot.Push(func(_ context.Context, req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
				want := &abci.RequestOfferSnapshot{Snapshot: toABCI(s), AppHash: []byte("app_hash")}
				utils.OrPanic(utils.TestDiff(want, req))
				return &abci.ResponseOfferSnapshot{Result: tc.result}, tc.err
			})

			err := rts.reactor.syncer.offerSnapshot(ctx, s)
			if tc.expectErr == unknownErr {
				require.Error(t, err)
			} else {
				unwrapped := errors.Unwrap(err)
				if unwrapped != nil {
					err = unwrapped
				}
				require.Equal(t, tc.expectErr, err)
			}
			app.AssertExpectations(t)
		})
	}
}

func TestSyncer_applyChunks_Results(t *testing.T) {
	unknownErr := errors.New("unknown error")
	boom := errors.New("boom")

	testcases := map[string]struct {
		result    abci.ResponseApplySnapshotChunk_Result
		err       error
		expectErr error
	}{
		"accept":           {abci.ResponseApplySnapshotChunk_ACCEPT, nil, nil},
		"abort":            {abci.ResponseApplySnapshotChunk_ABORT, nil, errAbort},
		"retry":            {abci.ResponseApplySnapshotChunk_RETRY, nil, nil},
		"retry_snapshot":   {abci.ResponseApplySnapshotChunk_RETRY_SNAPSHOT, nil, errRetrySnapshot},
		"reject_snapshot":  {abci.ResponseApplySnapshotChunk_REJECT_SNAPSHOT, nil, errRejectSnapshot},
		"unknown":          {abci.ResponseApplySnapshotChunk_UNKNOWN, nil, unknownErr},
		"error":            {0, boom, boom},
		"unknown non-zero": {9, nil, unknownErr},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			stateProvider := &mocks.StateProvider{}
			stateProvider.On("AppHash", mock.Anything, mock.Anything).Return([]byte("app_hash"), nil)

			rts := setup(t, nil, stateProvider, true)
			app := rts.conn

			body := []byte{1, 2, 3}
			chunks, err := newChunkQueue(&snapshot{Height: 1, Format: 1, Chunks: 1}, t.TempDir())
			require.NoError(t, err)

			fetchStartTime := time.Now()

			_, err = chunks.Add(&chunk{Height: 1, Format: 1, Index: 0, Chunk: body})
			require.NoError(t, err)

			app.applySnapshotChunk.Push(func(_ context.Context, req *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
				want := &abci.RequestApplySnapshotChunk{Index: 0, Chunk: body}
				utils.OrPanic(utils.TestDiff(want, req))
				return &abci.ResponseApplySnapshotChunk{Result: tc.result}, tc.err
			})
			if tc.result == abci.ResponseApplySnapshotChunk_RETRY {
				app.applySnapshotChunk.Push(mkHandler(
					&abci.RequestApplySnapshotChunk{Index: 0, Chunk: body},
					&abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ACCEPT},
				))
			}

			err = rts.reactor.syncer.applyChunks(ctx, chunks, fetchStartTime)
			if tc.expectErr == unknownErr {
				require.Error(t, err)
			} else {
				unwrapped := errors.Unwrap(err)
				if unwrapped != nil {
					err = unwrapped
				}
				require.Equal(t, tc.expectErr, err)
			}

			app.AssertExpectations(t)
		})
	}
}

func TestSyncer_applyChunks_RefetchChunks(t *testing.T) {
	// Discarding chunks via refetch_chunks should work the same for all results
	testcases := map[string]struct {
		result abci.ResponseApplySnapshotChunk_Result
	}{
		"accept":          {abci.ResponseApplySnapshotChunk_ACCEPT},
		"abort":           {abci.ResponseApplySnapshotChunk_ABORT},
		"retry":           {abci.ResponseApplySnapshotChunk_RETRY},
		"retry_snapshot":  {abci.ResponseApplySnapshotChunk_RETRY_SNAPSHOT},
		"reject_snapshot": {abci.ResponseApplySnapshotChunk_REJECT_SNAPSHOT},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()

			stateProvider := &mocks.StateProvider{}
			stateProvider.On("AppHash", mock.Anything, mock.Anything).Return([]byte("app_hash"), nil)

			rts := setup(t, nil, stateProvider, true)
			app := rts.conn

			chunks, err := newChunkQueue(&snapshot{Height: 1, Format: 1, Chunks: 3}, t.TempDir())
			require.NoError(t, err)

			fetchStartTime := time.Now()

			added, err := chunks.Add(&chunk{Height: 1, Format: 1, Index: 0, Chunk: []byte{0}})
			require.True(t, added)
			require.NoError(t, err)
			added, err = chunks.Add(&chunk{Height: 1, Format: 1, Index: 1, Chunk: []byte{1}})
			require.True(t, added)
			require.NoError(t, err)
			added, err = chunks.Add(&chunk{Height: 1, Format: 1, Index: 2, Chunk: []byte{2}})
			require.True(t, added)
			require.NoError(t, err)

			// The first two chunks are accepted, before the last one asks for 1 to be refetched
			app.applySnapshotChunk.Push(mkHandler(
				&abci.RequestApplySnapshotChunk{Index: 0, Chunk: []byte{0}},
				&abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ACCEPT},
			))
			app.applySnapshotChunk.Push(mkHandler(
				&abci.RequestApplySnapshotChunk{Index: 1, Chunk: []byte{1}},
				&abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ACCEPT},
			))
			app.applySnapshotChunk.Push(mkHandler(
				&abci.RequestApplySnapshotChunk{Index: 2, Chunk: []byte{2}},
				&abci.ResponseApplySnapshotChunk{Result: tc.result, RefetchChunks: []uint32{1}},
			))

			// Since removing the chunk will cause Next() to block, we spawn a goroutine, then
			// check the queue contents, and finally close the queue to end the goroutine.
			// We don't really care about the result of applyChunks, since it has separate test.
			done := make(chan struct{})
			go func() {
				defer close(done)
				// purposefully ignore error
				_ = rts.reactor.syncer.applyChunks(ctx, chunks, fetchStartTime)
			}()

			require.Eventually(t, func() bool {
				return chunks.Has(0) && !chunks.Has(1) && chunks.Has(2)
			}, 2*time.Second, 20*time.Millisecond)

			t.Cleanup(func() {
				err = chunks.Close()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					t.Errorf("applyChunks goroutine did not exit")
				}
				require.NoError(t, err)
				app.AssertExpectations(t)
			})
		})
	}
}

func TestSyncer_applyChunks_RejectSenders(t *testing.T) {
	// Banning chunks senders via ban_chunk_senders should work the same for all results
	testcases := map[string]struct {
		result abci.ResponseApplySnapshotChunk_Result
	}{
		"accept":          {abci.ResponseApplySnapshotChunk_ACCEPT},
		"abort":           {abci.ResponseApplySnapshotChunk_ABORT},
		"retry":           {abci.ResponseApplySnapshotChunk_RETRY},
		"retry_snapshot":  {abci.ResponseApplySnapshotChunk_RETRY_SNAPSHOT},
		"reject_snapshot": {abci.ResponseApplySnapshotChunk_REJECT_SNAPSHOT},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()

			stateProvider := &mocks.StateProvider{}
			stateProvider.On("AppHash", mock.Anything, mock.Anything).Return([]byte("app_hash"), nil)

			rts := setup(t, nil, stateProvider, true)
			app := rts.conn

			// Set up three peers across two snapshots, and ask for one of them to be banned.
			// It should be banned from all snapshots.
			peerAID := types.NodeID("aa")
			peerBID := types.NodeID("bb")
			peerCID := types.NodeID("cc")

			s1 := &snapshot{Height: 1, Format: 1, Chunks: 3}
			s2 := &snapshot{Height: 2, Format: 1, Chunks: 3}

			_, err := rts.reactor.syncer.AddSnapshot(peerAID, s1)
			require.NoError(t, err)

			_, err = rts.reactor.syncer.AddSnapshot(peerAID, s2)
			require.NoError(t, err)

			_, err = rts.reactor.syncer.AddSnapshot(peerBID, s1)
			require.NoError(t, err)

			_, err = rts.reactor.syncer.AddSnapshot(peerBID, s2)
			require.NoError(t, err)

			_, err = rts.reactor.syncer.AddSnapshot(peerCID, s1)
			require.NoError(t, err)

			_, err = rts.reactor.syncer.AddSnapshot(peerCID, s2)
			require.NoError(t, err)

			chunks, err := newChunkQueue(s1, t.TempDir())
			require.NoError(t, err)

			fetchStartTime := time.Now()

			added, err := chunks.Add(&chunk{Height: 1, Format: 1, Index: 0, Chunk: []byte{0}, Sender: peerAID})
			require.True(t, added)
			require.NoError(t, err)

			added, err = chunks.Add(&chunk{Height: 1, Format: 1, Index: 1, Chunk: []byte{1}, Sender: peerBID})
			require.True(t, added)
			require.NoError(t, err)

			added, err = chunks.Add(&chunk{Height: 1, Format: 1, Index: 2, Chunk: []byte{2}, Sender: peerCID})
			require.True(t, added)
			require.NoError(t, err)

			// The first two chunks are accepted, before the last one asks for b sender to be rejected
			app.applySnapshotChunk.Push(mkHandler(
				&abci.RequestApplySnapshotChunk{Index: 0, Chunk: []byte{0}, Sender: "aa"},
				&abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ACCEPT},
			))
			app.applySnapshotChunk.Push(mkHandler(
				&abci.RequestApplySnapshotChunk{Index: 1, Chunk: []byte{1}, Sender: "bb"},
				&abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ACCEPT},
			))
			app.applySnapshotChunk.Push(mkHandler(
				&abci.RequestApplySnapshotChunk{Index: 2, Chunk: []byte{2}, Sender: "cc"},
				&abci.ResponseApplySnapshotChunk{Result: tc.result, RejectSenders: []string{string(peerBID)}},
			))

			// On retry, the last chunk will be tried again, so we just accept it then.
			if tc.result == abci.ResponseApplySnapshotChunk_RETRY {
				app.applySnapshotChunk.Push(mkHandler(
					&abci.RequestApplySnapshotChunk{Index: 2, Chunk: []byte{2}, Sender: "cc"},
					&abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ACCEPT},
				))
			}

			// We don't really care about the result of applyChunks, since it has separate test.
			// However, it will block on e.g. retry result, so we spawn a goroutine that will
			// be shut down when the chunk queue closes.
			go func() {
				rts.reactor.syncer.applyChunks(ctx, chunks, fetchStartTime) //nolint:errcheck // purposefully ignore error
			}()

			time.Sleep(50 * time.Millisecond)

			s1peers := rts.reactor.syncer.snapshots.GetPeers(s1)
			if err := utils.TestDiff([]types.NodeID{peerAID, peerCID}, s1peers); err != nil {
				t.Fatal(err)
			}
			require.NoError(t, chunks.Close())
			app.AssertExpectations(t)
		})
	}
}

func TestSyncer_verifyApp(t *testing.T) {
	boom := errors.New("boom")
	const appVersion = 9
	appVersionMismatchErr := errors.New("app version mismatch. Expected: 9, got: 2")
	s := &snapshot{Height: 3, Format: 1, Chunks: 5, Hash: []byte{1, 2, 3}, trustedAppHash: []byte("app_hash")}

	testcases := map[string]struct {
		response  *abci.ResponseInfo
		err       error
		expectErr error
	}{
		"verified": {&abci.ResponseInfo{
			LastBlockHeight:  3,
			LastBlockAppHash: []byte("app_hash"),
			AppVersion:       appVersion,
		}, nil, nil},
		"invalid app version": {&abci.ResponseInfo{
			LastBlockHeight:  3,
			LastBlockAppHash: []byte("app_hash"),
			AppVersion:       2,
		}, nil, appVersionMismatchErr},
		"invalid height": {&abci.ResponseInfo{
			LastBlockHeight:  5,
			LastBlockAppHash: []byte("app_hash"),
			AppVersion:       appVersion,
		}, nil, errVerifyFailed},
		"invalid hash": {&abci.ResponseInfo{
			LastBlockHeight:  3,
			LastBlockAppHash: []byte("xxx"),
			AppVersion:       appVersion,
		}, nil, errVerifyFailed},
		"error": {nil, boom, boom},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()

			rts := setup(t, nil, nil, true)

			app := rts.conn
			app.info.Push(func(_ context.Context, req *abci.RequestInfo) (*abci.ResponseInfo, error) {
				utils.OrPanic(utils.TestDiff(&proxy.RequestInfo, req))
				return tc.response, tc.err
			})
			err := rts.reactor.syncer.verifyApp(ctx, s, appVersion)
			unwrapped := errors.Unwrap(err)
			if unwrapped != nil {
				err = unwrapped
			}
			require.Equal(t, tc.expectErr, err)
			app.AssertExpectations(t)
		})
	}
}

func toABCI(s *snapshot) *abci.Snapshot {
	return &abci.Snapshot{
		Height:   s.Height,
		Format:   s.Format,
		Chunks:   s.Chunks,
		Hash:     s.Hash,
		Metadata: s.Metadata,
	}
}

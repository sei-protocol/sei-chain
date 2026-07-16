//go:build !mock_chain_validation

// These tests drive a peer-supplied block whose commit fails verification and
// assert the routine evicts/retries instead of applying it. mock_chain_validation
// swallows the commit-verify failure (ErrLastCommitVerify) and applies the block;
// other builds keep the production eviction/retry path, so only that build is excluded.
package blocksync

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func makeValidationFailurePair(
	ctx context.Context,
	t *testing.T,
	testRootName string,
) (sm.State, *types.Block, *types.Block) {
	t.Helper()

	cfg, err := config.ResetTestRoot(t.TempDir(), testRootName)
	require.NoError(t, err)

	valSet, privVals := factory.ValidatorSet(ctx, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	initialState, err := sm.MakeGenesisState(genDoc)
	require.NoError(t, err)

	lastCommit := &types.Commit{}
	block1, _, _, seenCommit1 := makeNextBlock(ctx, t, initialState, privVals[0], 1, lastCommit)
	block2, _, _, _ := makeNextBlock(ctx, t, initialState, privVals[0], 2, seenCommit1)

	badBlock2Proto, err := block2.ToProto()
	require.NoError(t, err)
	badBlock2Proto.LastCommit.Signatures[0].Signature[0] ^= 0xFF
	badCommit, err := types.CommitFromProto(badBlock2Proto.LastCommit)
	require.NoError(t, err)
	badBlock2Proto.Header.LastCommitHash = badCommit.Hash()
	badBlock2, err := types.BlockFromProto(badBlock2Proto)
	require.NoError(t, err)

	return initialState, block1, badBlock2
}

func TestPoolRoutine_DoesNotReturnOnValidationFailure(t *testing.T) {
	ctx := t.Context()

	initialState, block1, badBlock2 := makeValidationFailurePair(ctx, t, "block_sync_validation_failure_does_not_return")

	badPeer := types.NodeID(strings.Repeat("a", 40))
	goodPeer := types.NodeID(strings.Repeat("b", 40))
	router := makeRouter(testPeers{
		badPeer:  {id: badPeer, base: 1, height: 2, inputChan: make(chan inputData, 1)},
		goodPeer: {id: goodPeer, base: 1, height: 2, inputChan: make(chan inputData, 1)},
	})
	pool := NewBlockPool(1, router)
	done := make(chan error, 1)
	go func() { done <- pool.run(ctx) }()
	t.Cleanup(func() {
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("pool.run(): %v", err)
		}
	})
	pool.SetPeerRange(badPeer, 1, 2)

	evictNetwork := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: 1})
	syncer := &syncController{
		router: evictNetwork.Node(evictNetwork.NodeIDs()[0]).Router,
	}

	results := make(chan error, 1)
	go func() {
		_, err := syncer.poolRoutine(ctx, pool, initialState, false)
		results <- err
	}()
	t.Cleanup(func() {
		err := <-results
		require.ErrorIs(t, err, context.Canceled)
	})

	introducedGoodPeer := false
	for {
		select {
		case err := <-results:
			t.Fatalf("poolRoutine returned early after validation failure: %v", err)
		case request := <-pool.Requests():
			if request.PeerID == goodPeer {
				return
			}

			switch request.Height {
			case 1:
				_ = pool.AddBlock(request.PeerID, block1, block1.Size())
			case 2:
				_ = pool.AddBlock(request.PeerID, badBlock2, badBlock2.Size())
				if !introducedGoodPeer {
					introducedGoodPeer = true
					pool.SetPeerRange(goodPeer, 1, 2)
				}
			}
		}
	}
}

func TestPoolRoutine_RetriesAfterValidationFailure(t *testing.T) {
	ctx := t.Context()

	initialState, block1, badBlock2 := makeValidationFailurePair(ctx, t, "block_sync_retry_after_validation_failure")
	network := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: 1})

	badPeer := types.NodeID(strings.Repeat("a", 40))
	goodPeer1 := types.NodeID(strings.Repeat("b", 40))
	goodPeer2 := types.NodeID(strings.Repeat("c", 40))
	peers := testPeers{
		badPeer:   {id: badPeer, base: 1, height: 2, inputChan: make(chan inputData, 1)},
		goodPeer1: {id: goodPeer1, base: 1, height: 2, inputChan: make(chan inputData, 1)},
		goodPeer2: {id: goodPeer2, base: 1, height: 2, inputChan: make(chan inputData, 1)},
	}
	pool := NewBlockPool(1, makeRouter(peers))
	runPoolForTest(t, pool)
	pool.SetPeerRange(badPeer, 1, 2)

	syncer := &syncController{
		router: network.Node(network.NodeIDs()[0]).Router,
	}

	results := make(chan error, 1)
	go func() {
		_, err := syncer.poolRoutine(ctx, pool, initialState, false)
		results <- err
	}()
	t.Cleanup(func() {
		err := <-results
		require.ErrorIs(t, err, context.Canceled)
	})

	introducedGoodPeers := false
	height1Requests := map[types.NodeID]int{}

	for {
		select {
		case err := <-results:
			t.Fatalf("poolRoutine returned before retry was observed: %v", err)
		case request := <-pool.Requests():
			if request.Height == 1 {
				height1Requests[request.PeerID]++
				if request.PeerID != badPeer && height1Requests[request.PeerID] == 1 {
					return
				}
			}

			if request.PeerID == badPeer && request.Height == 2 && !introducedGoodPeers {
				introducedGoodPeers = true
				pool.SetPeerRange(goodPeer1, 1, 2)
				pool.SetPeerRange(goodPeer2, 1, 2)
			}

			if request.PeerID == badPeer {
				switch request.Height {
				case 1:
					_ = pool.AddBlock(request.PeerID, block1, block1.Size())
				case 2:
					_ = pool.AddBlock(request.PeerID, badBlock2, badBlock2.Size())
				}
			}
		}
	}
}

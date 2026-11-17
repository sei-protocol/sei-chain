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

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// makeValidationFailurePair returns a one-validator genesis state, block 1, and a block 2
// whose commit for block 1 has a signature that does not verify.
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

	block1 := makeCommittedBlock(ctx, t, initialState, privVals, 1, &types.Commit{})
	block2 := makeCommittedBlock(ctx, t, initialState, privVals, 2, block1.commit)

	return initialState, block1.block, withBadLastSignature(t, block2.block, tmproto.BlockIDFlagCommit)
}

// withBadLastSignature returns a copy of block whose last commit has a last signature that
// does not verify, with flag as the block ID flag of that signature.
func withBadLastSignature(t *testing.T, block *types.Block, flag tmproto.BlockIDFlag) *types.Block {
	t.Helper()
	blockProto, err := block.ToProto()
	require.NoError(t, err)
	sigs := blockProto.LastCommit.Signatures
	lastSig := &sigs[len(sigs)-1]
	lastSig.Signature[0] ^= 0xFF
	lastSig.BlockIdFlag = flag
	badCommit, err := types.CommitFromProto(blockProto.LastCommit)
	require.NoError(t, err)
	blockProto.Header.LastCommitHash = badCommit.Hash()
	badBlock, err := types.BlockFromProto(blockProto)
	require.NoError(t, err)
	return badBlock
}

func TestPoolRoutine_DoesNotReturnOnValidationFailure(t *testing.T) {
	ctx := t.Context()

	initialState, block1, badBlock2 := makeValidationFailurePair(ctx, t, "block_sync_validation_failure_does_not_return")

	invalidCommitPeer := types.NodeID(strings.Repeat("a", 40))
	correctPeer := types.NodeID(strings.Repeat("b", 40))
	router := makeRouter(testPeers{
		invalidCommitPeer: {id: invalidCommitPeer, base: 1, height: 2, inputChan: make(chan inputData, 1)},
		correctPeer:       {id: correctPeer, base: 1, height: 2, inputChan: make(chan inputData, 1)},
	})
	pool := NewBlockPool(1, router)
	done := make(chan error, 1)
	go func() { done <- pool.run(ctx) }()
	t.Cleanup(func() {
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("pool.run(): %v", err)
		}
	})
	pool.SetPeerRange(invalidCommitPeer, 1, 2)

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

	introducedCorrectPeer := false
	for {
		select {
		case err := <-results:
			t.Fatalf("poolRoutine returned early after validation failure: %v", err)
		case request := <-pool.Requests():
			if request.PeerID == correctPeer {
				return
			}

			switch request.Height {
			case 1:
				_ = pool.AddBlock(request.PeerID, block1, block1.Size())
			case 2:
				_ = pool.AddBlock(request.PeerID, badBlock2, badBlock2.Size())
				if !introducedCorrectPeer {
					introducedCorrectPeer = true
					pool.SetPeerRange(correctPeer, 1, 2)
				}
			}
		}
	}
}

func TestPoolRoutine_RetriesAfterValidationFailure(t *testing.T) {
	ctx := t.Context()

	initialState, block1, badBlock2 := makeValidationFailurePair(ctx, t, "block_sync_retry_after_validation_failure")
	network := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: 1})

	invalidCommitPeer := types.NodeID(strings.Repeat("a", 40))
	correctPeer1 := types.NodeID(strings.Repeat("b", 40))
	correctPeer2 := types.NodeID(strings.Repeat("c", 40))
	peers := testPeers{
		invalidCommitPeer: {id: invalidCommitPeer, base: 1, height: 2, inputChan: make(chan inputData, 1)},
		correctPeer1:      {id: correctPeer1, base: 1, height: 2, inputChan: make(chan inputData, 1)},
		correctPeer2:      {id: correctPeer2, base: 1, height: 2, inputChan: make(chan inputData, 1)},
	}
	pool := NewBlockPool(1, makeRouter(peers))
	runPoolForTest(t, pool)
	pool.SetPeerRange(invalidCommitPeer, 1, 2)

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

	introducedCorrectPeers := false
	height1Requests := map[types.NodeID]int{}

	for {
		select {
		case err := <-results:
			t.Fatalf("poolRoutine returned before retry was observed: %v", err)
		case request := <-pool.Requests():
			if request.Height == 1 {
				height1Requests[request.PeerID]++
				if request.PeerID != invalidCommitPeer && height1Requests[request.PeerID] == 1 {
					return
				}
			}

			if request.PeerID == invalidCommitPeer && request.Height == 2 && !introducedCorrectPeers {
				introducedCorrectPeers = true
				pool.SetPeerRange(correctPeer1, 1, 2)
				pool.SetPeerRange(correctPeer2, 1, 2)
			}

			if request.PeerID == invalidCommitPeer {
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

// heightWatchStore is a block store that publishes the height of every block it saves.
type heightWatchStore struct {
	sm.BlockStore
	savedHeight *utils.AtomicSend[int64]
}

func (s heightWatchStore) SaveBlock(block *types.Block, blockParts *types.PartSet, seenCommit *types.Commit) {
	s.BlockStore.SaveBlock(block, blockParts, seenCommit)
	s.savedHeight.Store(block.Height)
}

// TestReactor_RejectsInvalidCommitFromPeer checks that a syncing  node saves the blocks an invalid-commit
// peer serves up to the first block whose commit fails verification, evicts that peer, and then
// still makes progress by saving that block from a correct peer.
func TestReactor_RejectsInvalidCommitFromPeer(t *testing.T) {
	for _, tc := range []struct {
		name string
		flag tmproto.BlockIDFlag
	}{
		{name: "commit vote", flag: tmproto.BlockIDFlagCommit},
		{name: "nil vote", flag: tmproto.BlockIDFlagNil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()

			cfg, err := config.ResetTestRoot(t.TempDir(), "block_sync_invalid_commit")
			require.NoError(t, err)
			valSet, privVals := factory.ValidatorSet(ctx, 4, 30)
			genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
			network := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: 2})
			nodeIDs := network.NodeIDs()
			invalidCommitID, syncID := nodeIDs[0], nodeIDs[1]
			syncNode := network.Node(syncID)

			// The syncing node publishes the height of every block it saves.
			savedHeight := utils.NewAtomicSend(int64(0))

			invalidCommitStore, correctStore := newMemBlockStore(), newMemBlockStore()
			invalidCommitReactor := makeReactor(ctx, t, genDoc, network.Node(invalidCommitID).Router, invalidCommitStore, false, func() {}, config.DefaultSelfRemediationConfig())
			syncReactor := makeReactor(ctx, t, genDoc, syncNode.Router,
				heightWatchStore{BlockStore: newMemBlockStore(), savedHeight: &savedHeight}, true, func() {}, config.DefaultSelfRemediationConfig())

			// Connect the invalid-commit node and the syncing node.
			network.Start(t)
			invalidCommitConn := syncNode.WaitForConnAndGet(ctx, invalidCommitID)

			// The correct node is not connected to anyone yet.
			correctNode := network.MakeNode(t, p2p.TestNodeOptions{})
			correctReactor := makeReactor(ctx, t, genDoc, correctNode.Router, correctStore, false, func() {}, config.DefaultSelfRemediationConfig())

			// The correct node applies and stores correct blocks 1 to 4.
			correctExec := correctReactor.syncer.OrPanic("syncer should be configured in tests").blockExec
			state, err := correctReactor.stateStore.Load()
			require.NoError(t, err)
			var correct []committedBlock
			lastCommit := &types.Commit{}
			for height := int64(1); height <= 4; height++ {
				b := makeCommittedBlock(ctx, t, state, privVals, height, lastCommit)
				state, err = correctExec.ApplyBlock(ctx, state, b.id, b.block, nil)
				require.NoError(t, err)
				correctStore.SaveBlock(b.block, b.parts, b.commit)
				correct = append(correct, b)
				lastCommit = b.commit
			}

			// The invalid-commit node has the state of the correct node. It stores blocks 1 to 3, and a block 4
			// whose commit for block 3 is invalid.
			require.NoError(t, invalidCommitReactor.stateStore.Save(state))
			for _, b := range correct[:3] {
				invalidCommitStore.SaveBlock(b.block, b.parts, b.commit)
			}
			badBlock4 := withBadLastSignature(t, correct[3].block, tc.flag)
			badParts4, err := badBlock4.MakePartSet(types.BlockPartSizeBytes)
			require.NoError(t, err)
			invalidCommitStore.SaveBlock(badBlock4, badParts4, correct[3].commit)

			// Start the reactors.
			for _, r := range []*Reactor{correctReactor, invalidCommitReactor, syncReactor} {
				require.NoError(t, r.Start(ctx))
				t.Cleanup(r.Wait)
			}

			// The syncing node saves blocks 1 and 2, rejects block 3, and closes its connection to the
			// invalid-commit node. Its store then ends at block 2, with the correct seen commit of block 2.
			syncNode.WaitForDisconnect(ctx, invalidCommitConn)
			require.Equal(t, int64(2), syncReactor.store.Height())
			require.NoError(t, utils.TestDiff(correct[1].block.Hash(), syncReactor.store.LoadBlock(2).Hash()))
			require.NoError(t, utils.TestDiff(correct[1].commit.Hash(), syncReactor.store.LoadSeenCommit().Hash()))

			// Stop the invalid-commit node and remove it from the network.
			invalidCommitReactor.Stop()
			network.Remove(t, invalidCommitID)

			// The correct node connects, and the syncing node saves block 3 with its correct seen commit.
			require.NoError(t, correctNode.Connect(ctx, syncNode))
			_, err = savedHeight.Wait(ctx, func(h int64) bool { return h >= 3 })
			require.NoError(t, err)
			require.Equal(t, int64(3), syncReactor.store.Height())
			require.NoError(t, utils.TestDiff(correct[2].block.Hash(), syncReactor.store.LoadBlock(3).Hash()))
			require.NoError(t, utils.TestDiff(correct[2].commit.Hash(), syncReactor.store.LoadSeenCommit().Hash()))
		})
	}
}

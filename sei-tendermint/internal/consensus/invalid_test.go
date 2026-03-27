package consensus

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	cstypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/consensus/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/bits"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	tmrand "github.com/sei-protocol/sei-chain/sei-tendermint/libs/rand"
	tmtime "github.com/sei-protocol/sei-chain/sei-tendermint/libs/time"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmcons "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/consensus"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
)

// Test checking that if peer sends a ProposalPOLMessage with a bitarray with bad length,
// the node will handle it gracefully.
func TestGossipVotesForHeightPoisonedProposalPOL(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	cfg := configSetup(t)
	states, cleanup := makeConsensusState(ctx, t, cfg, 2, "consensus_reactor_test", newMockTickerFunc(true))
	t.Cleanup(cleanup)

	rts := setup(ctx, t, 2, states, 1)

	var nodeIDs []types.NodeID
	for _, node := range rts.network.Nodes() {
		nodeIDs = append(nodeIDs, node.NodeID)
	}
	require.Len(t, nodeIDs, 2)

	reactor := rts.reactors[nodeIDs[0]]
	peerID := nodeIDs[1]
	state := reactor.state.GetState()
	reactor.SwitchToConsensus(ctx, state, false)

	require.Eventually(t, func() bool {
		_, ok := reactor.GetPeerState(peerID)
		return ok
	}, time.Hour, 50*time.Millisecond)

	valSet, privVals := types.RandValidatorSet(4, 1)
	proposerPubKey, err := privVals[0].GetPubKey(ctx)
	require.NoError(t, err)

	proposal := types.NewProposal(
		1,
		1,
		0,
		types.BlockID{
			Hash: crypto.CRandBytes(crypto.HashSize),
			PartSetHeader: types.PartSetHeader{
				Total: 1,
				Hash:  crypto.CRandBytes(crypto.HashSize),
			},
		},
		time.Now(),
		nil,
		types.Header{
			Version:         version.Consensus{Block: version.BlockProtocol},
			Height:          1,
			ProposerAddress: proposerPubKey.Address(),
		},
		&types.Commit{},
		nil,
		proposerPubKey.Address(),
	)
	proposal.Signature = makeSig("invalid-signature")

	require.NoError(t, reactor.handleStateMessage(p2p.RecvMsg[*tmcons.Message]{
		From: peerID,
		Message: MsgToProto(&NewRoundStepMessage{
			HRS: cstypes.HRS{
				Height: 1,
				Round:  1,
				Step:   cstypes.RoundStepPrevote,
			},
			SecondsSinceStartTime: 1,
			LastCommitRound:       -1,
		}),
	}))

	require.NoError(t, reactor.handleDataMessage(ctx, p2p.RecvMsg[*tmcons.Message]{
		From: peerID,
		Message: MsgToProto(&ProposalMessage{
			Proposal: proposal,
		}),
	}))

	require.NoError(t, reactor.handleDataMessage(ctx, p2p.RecvMsg[*tmcons.Message]{
		From: peerID,
		Message: MsgToProto(&ProposalPOLMessage{
			Height:           1,
			ProposalPOLRound: 0,
			ProposalPOL:      bits.NewBitArray(1),
		}),
	}))

	ps, ok := reactor.GetPeerState(peerID)
	require.True(t, ok)
	prs := ps.GetRoundState()
	require.Equal(t, int64(1), prs.Height)
	require.Equal(t, int32(1), prs.Round)
	require.Equal(t, int32(0), prs.ProposalPOLRound)
	require.Equal(t, 1, prs.ProposalPOL.Size())

	voteSet := cstypes.NewHeightVoteSet("test-chain", 1, valSet)
	voteSet.SetRound(1)

	voter := newValidatorStub(privVals[1], 1)
	voter.Height = 1
	voter.Round = 0

	vote := signVote(ctx, t, voter, tmproto.PrevoteType, "test-chain", types.BlockID{
		Hash: crypto.CRandBytes(crypto.HashSize),
		PartSetHeader: types.PartSetHeader{
			Total: 1,
			Hash:  crypto.CRandBytes(crypto.HashSize),
		},
	})
	added, err := voteSet.AddVote(vote, "")
	require.NoError(t, err)
	require.True(t, added)

	rs := &cstypes.RoundState{
		HRS: cstypes.HRS{
			Height: 1,
			Round:  1,
			Step:   cstypes.RoundStepPrevote,
		},
		Votes: voteSet,
	}

	// Gossip should take into consideration that PeerState might contain
	// invalid length bitarrays.
	for range 10 {
		reactor.gossipVotesForHeight(rs, ps.GetRoundState(), ps)
	}
}

func TestReactorInvalidPrecommit(t *testing.T) {
	t.Skip("test doesn't check anything useful")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	config := configSetup(t)

	const n = 4
	states, cleanup := makeConsensusState(ctx, t,
		config, n, "consensus_reactor_test",
		newMockTickerFunc(true))
	t.Cleanup(cleanup)

	for i := range n {
		ticker := NewTimeoutTicker()
		states[i].SetTimeoutTicker(ticker)
	}

	t.Logf("setup()")
	rts := setup(ctx, t, n, states, 1) // buffer must be large enough to not deadlock
	t.Logf("setup() done")

	for _, reactor := range rts.reactors {
		state := reactor.state.GetState()
		reactor.SwitchToConsensus(ctx, state, false)
	}

	// this val sends a random precommit at each height
	node := rts.network.RandomNode()

	byzState := rts.states[node.NodeID]
	byzReactor := rts.reactors[node.NodeID]

	signal := make(chan struct{})
	// Update the doPrevote function to just send a valid precommit for a random
	// block and otherwise disable the priv validator.
	byzState.mtx.Lock()
	privVal, ok := byzState.privValidator.Get()
	if !ok {
		t.Fatal("privValidator not found")
	}
	byzState.doPrevote = func(ctx context.Context, height int64, round int32) {
		defer close(signal)
		invalidDoPrevoteFunc(ctx, t, byzState, byzReactor, rts.voteChannels[node.NodeID], privVal)
	}
	byzState.mtx.Unlock()

	t.Log("wait for a bunch of blocks")
	// TODO: Make this tighter by ensuring the halt happens by block 2.
	var wg sync.WaitGroup

	for range 10 {
		for _, sub := range rts.subs {
			wg.Add(1)

			go func(s eventbus.Subscription) {
				defer wg.Done()
				_, err := s.Next(ctx)
				if ctx.Err() != nil {
					return
				}
				t.Log("BLOCK")
				if !assert.NoError(t, err) {
					cancel() // cancel other subscribers on failure
				}
			}(sub)
		}
	}
	wait := make(chan struct{})
	go func() { defer close(wait); wg.Wait() }()

	select {
	case <-wait:
		if _, ok := <-signal; !ok {
			t.Fatal("test condition did not fire")
		}
	case <-ctx.Done():
		if _, ok := <-signal; !ok {
			t.Fatal("test condition did not fire after timeout")
			return
		}
	case <-signal:
		// test passed
	}
}

func invalidDoPrevoteFunc(
	ctx context.Context,
	t *testing.T,
	cs *State,
	r *Reactor,
	voteCh *p2p.Channel[*tmcons.Message],
	pv types.PrivValidator,
) {
	// routine to:
	// - precommit for a random block
	// - send precommit to all peers
	// - disable privValidator (so we don't do normal precommits)
	go func() {
		cs.mtx.Lock()
		cs.privValidator = utils.Some(pv)

		pubKey, err := pv.GetPubKey(ctx)
		require.NoError(t, err)

		addr := pubKey.Address()
		valIndex, _, ok := cs.roundState.Validators().GetByAddress(addr)
		if !ok {
			panic("mikssing validator")
		}

		// precommit a random block
		blockHash := bytes.HexBytes(tmrand.Bytes(32))
		precommit := &types.Vote{
			ValidatorAddress: addr,
			ValidatorIndex:   valIndex,
			Height:           cs.roundState.Height(),
			Round:            cs.roundState.Round(),
			Timestamp:        tmtime.Now(),
			Type:             tmproto.PrecommitType,
			BlockID: types.BlockID{
				Hash:          blockHash,
				PartSetHeader: types.PartSetHeader{Total: 1, Hash: tmrand.Bytes(32)},
			},
		}

		p := precommit.ToProto()
		require.NoError(t, pv.SignVote(ctx, cs.state.ChainID, p))
		precommit.Signature = utils.Some(utils.OrPanic1(crypto.SigFromBytes(p.Signature)))
		t.Logf("disable priv val so we don't do normal votes")
		cs.privValidator = utils.None[types.PrivValidator]()
		cs.mtx.Unlock()

		var ids []types.NodeID
		for peers := range r.peers.RLock() {
			for _, ps := range peers {
				ids = append(ids, ps.peerID)
			}
		}

		count := 0
		for _, peerID := range ids {
			count++
			voteCh.Send(MsgToProto(&VoteMessage{Vote: precommit}), peerID)
			// we want to have sent some of these votes,
			// but if the test completes without erroring
			// or not sending any messages, then we should
			// error.
			if errors.Is(err, context.Canceled) && count > 0 {
				break
			}
			require.NoError(t, err)
		}
	}()
}

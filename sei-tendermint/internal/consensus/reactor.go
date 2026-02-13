package consensus

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	cstypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/consensus/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/bits"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/service"
	tmtime "github.com/sei-protocol/sei-chain/sei-tendermint/libs/time"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	tmcons "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/consensus"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var (
	_ service.Service = (*Reactor)(nil)
)

type desc = p2p.ChannelDescriptor[*tmcons.Message]

func GetStateChannelDescriptor() desc {
	return desc{
		ID:                  StateChannel,
		MessageType:         new(tmcons.Message),
		Priority:            8,
		SendQueueCapacity:   64,
		RecvMessageCapacity: maxMsgSize,
		RecvBufferCapacity:  128,
		Name:                "state",
	}
}

func GetDataChannelDescriptor() desc {
	return desc{
		// TODO: Consider a split between gossiping current block and catchup
		// stuff. Once we gossip the whole block there is nothing left to send
		// until next height or round.
		ID:                  DataChannel,
		MessageType:         new(tmcons.Message),
		Priority:            12,
		SendQueueCapacity:   64,
		RecvBufferCapacity:  512,
		RecvMessageCapacity: maxMsgSize,
		Name:                "data",
	}
}

func GetVoteChannelDescriptor() desc {
	return desc{
		ID:                  VoteChannel,
		MessageType:         new(tmcons.Message),
		Priority:            10,
		SendQueueCapacity:   64,
		RecvBufferCapacity:  128,
		RecvMessageCapacity: maxMsgSize,
		Name:                "vote",
	}
}

func GetVoteSetChannelDescriptor() desc {
	return desc{
		ID:                  VoteSetBitsChannel,
		MessageType:         new(tmcons.Message),
		Priority:            5,
		SendQueueCapacity:   8,
		RecvBufferCapacity:  128,
		RecvMessageCapacity: maxMsgSize,
		Name:                "voteSet",
	}
}

const (
	StateChannel       = p2p.ChannelID(0x20)
	DataChannel        = p2p.ChannelID(0x21)
	VoteChannel        = p2p.ChannelID(0x22)
	VoteSetBitsChannel = p2p.ChannelID(0x23)

	maxMsgSize = 4194304 // 4MB; NOTE: keep larger than types.PartSet sizes.
)

// Reactor defines a reactor for the consensus service.
type Reactor struct {
	service.BaseService
	logger log.Logger
	cfg    *config.Config

	state    *State
	router   *p2p.Router
	channels channelBundle
	eventBus *eventbus.EventBus
	Metrics  *Metrics

	peers       utils.RWMutex[map[types.NodeID]*PeerState]
	roundState  atomic.Pointer[cstypes.RoundState]
	readySignal utils.AtomicSend[bool]
}

// NewReactor returns a reference to a new consensus reactor, which implements
// the service.Service interface. It accepts a logger, consensus state, references
// to relevant p2p Channels and a channel to listen for peer updates on. The
// reactor will close all p2p Channels when stopping.
func NewReactor(
	logger log.Logger,
	cs *State,
	router *p2p.Router,
	eventBus *eventbus.EventBus,
	waitSync bool,
	metrics *Metrics,
	cfg *config.Config,
) (*Reactor, error) {
	stateCh, err := p2p.OpenChannel(router, GetStateChannelDescriptor())
	if err != nil {
		return nil, err
	}
	dataCh, err := p2p.OpenChannel(router, GetDataChannelDescriptor())
	if err != nil {
		return nil, err
	}
	voteCh, err := p2p.OpenChannel(router, GetVoteChannelDescriptor())
	if err != nil {
		return nil, err
	}
	voteSetCh, err := p2p.OpenChannel(router, GetVoteSetChannelDescriptor())
	if err != nil {
		return nil, err
	}
	r := &Reactor{
		logger:      logger,
		state:       cs,
		peers:       utils.NewRWMutex(map[types.NodeID]*PeerState{}),
		eventBus:    eventBus,
		Metrics:     metrics,
		router:      router,
		readySignal: utils.NewAtomicSend(!waitSync),
		channels: channelBundle{
			state:  stateCh,
			data:   dataCh,
			vote:   voteCh,
			votSet: voteSetCh,
		},
		cfg: cfg,
	}
	r.roundState.Store(cs.GetRoundState())
	cs.eventNewRoundStep = r.broadcastNewRoundStepMessage
	cs.eventVote = r.broadcastHasVoteMessage
	cs.eventMsg = r.recordPeerMsg
	r.BaseService = *service.NewBaseService(logger, "Consensus", r)

	return r, nil
}

type channelBundle struct {
	state  *p2p.Channel[*tmcons.Message]
	data   *p2p.Channel[*tmcons.Message]
	vote   *p2p.Channel[*tmcons.Message]
	votSet *p2p.Channel[*tmcons.Message]
}

// OnStart starts separate go routines for each p2p Channel and listens for
// envelopes on each. In addition, it also listens for peer updates and handles
// messages on that p2p channel accordingly. The caller must be sure to execute
// OnStop to ensure the outbound p2p Channels are closed.
func (r *Reactor) OnStart(ctx context.Context) error {
	r.logger.Debug("consensus wait sync", "wait_sync", r.WaitSync())

	if err := r.state.updateStateFromStore(); err != nil {
		return err
	}
	r.SpawnCritical("updateRoundStateRoutine", r.updateRoundStateRoutine)
	// All of the channel processing routines are noops until readySignal,
	// but they are spawned immediately so that they keep the channels empty until then.
	r.SpawnCritical("processStateCh", r.processStateCh)
	r.SpawnCritical("processDataCh", r.processDataCh)
	r.SpawnCritical("processVoteCh", r.processVoteCh)
	r.SpawnCritical("processVoteSetBitsCh", r.processVoteSetBitsCh)
	r.SpawnCritical("state.Run", func(ctx context.Context) error {
		if _, err := r.readySignal.Wait(ctx, func(ready bool) bool { return ready }); err != nil {
			return err
		}
		r.SpawnCritical("processPeerUpdates", r.processPeerUpdates)
		r.SpawnCritical("broadcastValidBlock", r.broadcastValidBlockRoutine)
		return r.state.Run(ctx)
	})
	return nil
}

// OnStop stops the reactor by signaling to all spawned goroutines to exit and
// blocking until they all exit, as well as unsubscribing from events and stopping
// state.
func (r *Reactor) OnStop() {}

// WaitSync returns whether the consensus reactor is waiting for state/block sync.
func (r *Reactor) WaitSync() bool {
	return !r.readySignal.Load()
}

// SwitchToConsensus switches from block-sync mode to consensus mode. It resets
// the state, turns off block-sync, and starts the consensus state-machine.
func (r *Reactor) SwitchToConsensus(ctx context.Context, state sm.State, skipWAL bool) {
	r.logger.Info("switching to consensus")

	d := types.EventDataBlockSyncStatus{Complete: true, Height: state.LastBlockHeight}
	if err := r.eventBus.PublishEventBlockSyncStatus(d); err != nil {
		r.logger.Error("failed to emit the blocksync complete event", "err", err)
	}
	r.Metrics.BlockSyncing.Set(0)
	r.Metrics.StateSyncing.Set(0)

	// we have no votes, so reconstruct LastCommit from SeenCommit
	r.state.mtx.Lock()
	if state.LastBlockHeight > 0 {
		r.state.reconstructLastCommit(state)
	}
	r.state.updateToState(state)
	if skipWAL {
		r.state.doWALCatchup = false
	}
	r.state.mtx.Unlock()

	r.readySignal.Store(true)
}

// String returns a string representation of the Reactor.
func (r *Reactor) String() string {
	return "ConsensusReactor"
}

// GetPeerState returns PeerState for a given NodeID.
func (r *Reactor) GetPeerState(peerID types.NodeID) (*PeerState, bool) {
	for peers := range r.peers.RLock() {
		ps, ok := peers[peerID]
		return ps, ok
	}
	panic("unreachable")
}

func (r *Reactor) broadcastNewRoundStepMessage(rs *cstypes.RoundState) {
	r.channels.state.Broadcast(MsgToProto(makeRoundStepMessage(rs)))
}

// Broadcasts NewValidBlockMessage whenever new valid block is reported.
// It rebroadcasts the NewValidBlockMessage periodically to ensure that peers know which parts
// we are missing. It is critical in case we have small number of peers (for example just 1),
// and they are overloaded (they drop messages a lot).
func (r *Reactor) broadcastValidBlockRoutine(ctx context.Context) error {
	// Rebroadcasting is a fallback mechanism, no need to expose the frequency as
	// a config parameter.
	const interval = time.Second
	return r.state.eventValidBlock.Iter(ctx, func(ctx context.Context, mrs utils.Option[*cstypes.RoundState]) error {
		rs, ok := mrs.Get()
		if !ok || rs.ProposalBlockParts == nil {
			return nil
		}
		for {
			r.channels.state.Broadcast(MsgToProto(&NewValidBlockMessage{
				Height:             rs.Height,
				Round:              rs.Round,
				BlockPartSetHeader: rs.ProposalBlockParts.Header(),
				// Block parts bit array might be updated between iterations,
				// so we need to reconstruct the message each time.
				BlockParts: rs.ProposalBlockParts.BitArray(),
				IsCommit:   rs.Step == cstypes.RoundStepCommit,
			}))
			if err := utils.Sleep(ctx, interval); err != nil {
				return err
			}
		}
	})
}

func (r *Reactor) broadcastHasVoteMessage(vote *types.Vote) {
	r.channels.state.Broadcast(MsgToProto(&HasVoteMessage{
		Height: vote.Height,
		Round:  vote.Round,
		Type:   vote.Type,
		Index:  vote.ValidatorIndex,
	}))
}

func makeRoundStepMessage(rs *cstypes.RoundState) *NewRoundStepMessage {
	return &NewRoundStepMessage{
		HRS:                   rs.HRS,
		SecondsSinceStartTime: int64(time.Since(rs.StartTime).Seconds()),
		LastCommitRound:       rs.LastCommit.GetRound(),
	}
}

func (r *Reactor) sendNewRoundStepMessage(peerID types.NodeID) {
	r.channels.state.Send(MsgToProto(makeRoundStepMessage(r.roundState.Load())), peerID)
}

func (r *Reactor) updateRoundStateRoutine(ctx context.Context) error {
	t := time.NewTicker(100 * time.Microsecond)
	for {
		if _, err := utils.Recv(ctx, t.C); err != nil {
			return err
		}
		r.roundState.Store(r.state.GetRoundState())
	}
}

func (r *Reactor) gossipDataForCatchup(rs *cstypes.RoundState, prs *cstypes.PeerRoundState, ps *PeerState) {
	logger := r.logger.With("height", prs.Height).With("peer", ps.peerID)

	if index, ok := prs.ProposalBlockParts.Not().PickRandom(); ok {
		// ensure that the peer's PartSetHeader is correct
		blockMeta := r.state.blockStore.LoadBlockMeta(prs.Height)
		if blockMeta == nil {
			logger.Error(
				"failed to load block meta",
				"our_height", rs.Height,
				"blockstore_base", r.state.blockStore.Base(),
				"blockstore_height", r.state.blockStore.Height(),
			)

			time.Sleep(r.state.config.PeerGossipSleepDuration)
			return
		} else if !blockMeta.BlockID.PartSetHeader.Equals(prs.ProposalBlockPartSetHeader) {
			logger.Info(
				"peer ProposalBlockPartSetHeader mismatch; sleeping",
				"block_part_set_header", blockMeta.BlockID.PartSetHeader,
				"peer_block_part_set_header", prs.ProposalBlockPartSetHeader,
			)

			time.Sleep(r.state.config.PeerGossipSleepDuration)
			return
		}

		part := r.state.blockStore.LoadBlockPart(prs.Height, index)
		if part == nil {
			logger.Error(
				"failed to load block part",
				"index", index,
				"block_part_set_header", blockMeta.BlockID.PartSetHeader,
				"peer_block_part_set_header", prs.ProposalBlockPartSetHeader,
			)

			time.Sleep(r.state.config.PeerGossipSleepDuration)
			return
		}

		logger.Debug("sending block part for catchup", "round", prs.Round, "index", index)
		r.channels.data.Send(MsgToProto(&BlockPartMessage{
			Height: prs.Height, // not our height, so it does not matter.
			Round:  prs.Round,  // not our height, so it does not matter
			Part:   part,
		}), ps.peerID)
		return
	}

	time.Sleep(r.state.config.PeerGossipSleepDuration)
}

func (r *Reactor) gossipDataRoutine(ctx context.Context, ps *PeerState) error {
	dataCh := r.channels.data
	logger := r.logger.With("peer", ps.peerID)

	timer := time.NewTimer(0)
	defer timer.Stop()

OUTER_LOOP:
	for {
		timer.Reset(r.state.config.PeerGossipSleepDuration)
		if _, err := utils.Recv(ctx, timer.C); err != nil {
			return err
		}

		rs := r.roundState.Load()
		prs := ps.GetRoundState()

		// Send proposal Block parts?
		if rs.ProposalBlockParts.HasHeader(prs.ProposalBlockPartSetHeader) {
			if index, ok := rs.ProposalBlockParts.BitArray().Sub(prs.ProposalBlockParts.Copy()).PickRandom(); ok {

				logger.Debug("sending block part", "height", prs.Height, "round", prs.Round)
				dataCh.Send(MsgToProto(&BlockPartMessage{
					Height: rs.Height, // this tells peer that this part applies to us
					Round:  rs.Round,  // this tells peer that this part applies to us
					Part:   rs.ProposalBlockParts.GetPart(index),
				}), ps.peerID)
				ps.SetHasProposalBlockPart(prs.Height, prs.Round, index)
				continue OUTER_LOOP
			}
		}

		// if the peer is on a previous height that we have, help catch up
		blockStoreBase := r.state.blockStore.Base()
		if blockStoreBase > 0 && 0 < prs.Height && prs.Height < rs.Height && prs.Height >= blockStoreBase {
			heightLogger := logger.With("height", prs.Height)

			// If we never received the commit message from the peer, the block parts
			// will not be initialized.
			if prs.ProposalBlockParts == nil {
				blockMeta := r.state.blockStore.LoadBlockMeta(prs.Height)
				if blockMeta == nil {
					heightLogger.Error(
						"failed to load block meta",
						"blockstoreBase", blockStoreBase,
						"blockstoreHeight", r.state.blockStore.Height(),
					)
				} else {
					ps.InitProposalBlockParts(blockMeta.BlockID.PartSetHeader)
				}

				// Continue the loop since prs is a copy and not effected by this
				// initialization.
				continue OUTER_LOOP
			}

			r.gossipDataForCatchup(rs, prs, ps)
			continue OUTER_LOOP
		}

		// if height and round don't match, sleep
		if (rs.Height != prs.Height) || (rs.Round != prs.Round) {
			continue OUTER_LOOP
		}

		// By here, height and round match.
		// Proposal block parts were already matched and sent if any were wanted.
		// (These can match on hash so the round doesn't matter)
		// Now consider sending other things, like the Proposal itself.

		// Send Proposal && ProposalPOL BitArray?
		if rs.Proposal != nil && !prs.Proposal {
			// Proposal: share the proposal metadata with peer.
			dataCh.Send(MsgToProto(&ProposalMessage{Proposal: rs.Proposal}), ps.peerID)
			// NOTE: A peer might have received a different proposal message, so
			// this Proposal msg will be rejected!
			ps.SetHasProposal(rs.Proposal)

			// ProposalPOL: lets peer know which POL votes we have so far. The peer
			// must receive ProposalMessage first. Note, rs.Proposal was validated,
			// so rs.Proposal.POLRound <= rs.Round, so we definitely have
			// rs.Votes.Prevotes(rs.Proposal.POLRound).
			if 0 <= rs.Proposal.POLRound {
				logger.Debug("sending POL", "height", prs.Height, "round", prs.Round)
				dataCh.Send(MsgToProto(&ProposalPOLMessage{
					Height:           rs.Height,
					ProposalPOLRound: rs.Proposal.POLRound,
					ProposalPOL:      rs.Votes.Prevotes(rs.Proposal.POLRound).BitArray(),
				}), ps.peerID)
			}
		}
	}
}

// pickSendVote picks a vote and sends it to the peer. It will return true if
// there is a vote to send and false otherwise.
func (r *Reactor) pickSendVote(ps *PeerState, votes types.VoteSetReader) bool {
	vote, ok := ps.PickVoteToSend(votes)
	if !ok {
		return false
	}

	if r.cfg.LogLevel == log.LogLevelDebug {
		psJson, err := ps.ToJSON() // expensive, so we only want to call if debug is on
		if err != nil {
			panic(fmt.Errorf("ps.ToJSON(): %w", err))
		}
		r.logger.Debug("sending vote message", "ps", string(psJson), "vote", vote)
	}
	r.channels.vote.Send(MsgToProto(&VoteMessage{Vote: vote}), ps.peerID)

	if err := ps.SetHasVote(vote); err != nil {
		panic(fmt.Errorf("ps.SetHasVote(): %w", err))
	}

	return true
}

func (r *Reactor) gossipVotesForHeight(
	rs *cstypes.RoundState,
	prs *cstypes.PeerRoundState,
	ps *PeerState,
) bool {
	logger := r.logger.With("height", prs.Height).With("peer", ps.peerID)

	// if there are lastCommits to send...
	if prs.Step == cstypes.RoundStepNewHeight {
		if ok := r.pickSendVote(ps, rs.LastCommit); ok {
			logger.Debug("picked rs.LastCommit to send")
			return true
		}
	}

	// if there are POL prevotes to send...
	if prs.Step <= cstypes.RoundStepPropose && prs.Round != -1 && prs.Round <= rs.Round && prs.ProposalPOLRound != -1 {
		if polPrevotes := rs.Votes.Prevotes(prs.ProposalPOLRound); polPrevotes != nil {
			if r.pickSendVote(ps, polPrevotes) {
				logger.Debug("picked rs.Prevotes(prs.ProposalPOLRound) to send", "round", prs.ProposalPOLRound)
				return true
			}
		}
	}

	// if there are prevotes to send...
	if prs.Step <= cstypes.RoundStepPrevoteWait && prs.Round != -1 && prs.Round <= rs.Round {
		if r.pickSendVote(ps, rs.Votes.Prevotes(prs.Round)) {
			logger.Debug("picked rs.Prevotes(prs.Round) to send", "round", prs.Round)
			return true
		}
	}

	// if there are precommits to send...
	if prs.Step <= cstypes.RoundStepPrecommitWait && prs.Round != -1 && prs.Round <= rs.Round {
		if r.pickSendVote(ps, rs.Votes.Precommits(prs.Round)) {
			logger.Debug("picked rs.Precommits(prs.Round) to send", "round", prs.Round)
			return true
		}
	}

	// if there are prevotes to send...(which are needed because of validBlock mechanism)
	if prs.Round != -1 && prs.Round <= rs.Round {
		if r.pickSendVote(ps, rs.Votes.Prevotes(prs.Round)) {
			logger.Debug("picked rs.Prevotes(prs.Round) to send", "round", prs.Round)
			return true
		}
	}

	// if there are POLPrevotes to send...
	if prs.ProposalPOLRound != -1 {
		if polPrevotes := rs.Votes.Prevotes(prs.ProposalPOLRound); polPrevotes != nil {
			if r.pickSendVote(ps, polPrevotes) {
				logger.Debug("picked rs.Prevotes(prs.ProposalPOLRound) to send", "round", prs.ProposalPOLRound)
				return true
			}
		}
	}

	return false
}

func (r *Reactor) gossipVotesRoutine(ctx context.Context, ps *PeerState) error {
	logger := r.logger.With("peer", ps.peerID)

	timer := time.NewTimer(0)
	defer timer.Stop()

	for ctx.Err() == nil {
		rs := r.roundState.Load()
		prs := ps.GetRoundState()

		// if height matches, then send LastCommit, Prevotes, and Precommits
		if rs.Height == prs.Height {
			if r.gossipVotesForHeight(rs, prs, ps) {
				continue
			}
		}

		// special catchup logic -- if peer is lagging by height 1, send LastCommit
		if prs.Height != 0 && rs.Height == prs.Height+1 {
			if r.pickSendVote(ps, rs.LastCommit) {
				logger.Debug("picked rs.LastCommit to send", "height", prs.Height)
				continue
			}
		}

		// catchup logic -- if peer is lagging by more than 1, send Commit
		blockStoreBase := r.state.blockStore.Base()

		if blockStoreBase > 0 && prs.Height != 0 && rs.Height >= prs.Height+2 && prs.Height >= blockStoreBase {
			// Load the block's extended commit for prs.Height, which contains precommit
			// signatures for prs.Height.
			r.state.mtx.RLock()
			ec := r.state.blockStore.LoadBlockCommit(prs.Height)
			r.state.mtx.RUnlock()
			if ec == nil {
				continue
			}
			if r.pickSendVote(ps, ec) {
				logger.Debug("picked Catchup commit to send", "height", prs.Height)
				continue
			}
		}

		timer.Reset(r.state.config.PeerGossipSleepDuration)
		if _, err := utils.Recv(ctx, timer.C); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// NOTE: `queryMaj23Routine` has a simple crude design since it only comes
// into play for liveness when there's a signature DDoS attack happening.
func (r *Reactor) queryMaj23Routine(ctx context.Context, ps *PeerState) error {
	stateCh := r.channels.state
	for {
		// TODO create more reliable copies of these
		// structures so the following go routines don't race
		rs := r.roundState.Load()
		prs := ps.GetRoundState()

		if rs.Height == prs.Height {
			// maybe send Height/Round/Prevotes
			if maj23, ok := rs.Votes.Prevotes(prs.Round).TwoThirdsMajority(); ok {
				stateCh.Send(MsgToProto(&VoteSetMaj23Message{
					Height:  prs.Height,
					Round:   prs.Round,
					Type:    tmproto.PrevoteType,
					BlockID: maj23,
				}), ps.peerID)
			}

			if prs.ProposalPOLRound >= 0 {
				// maybe send Height/Round/ProposalPOL
				if maj23, ok := rs.Votes.Prevotes(prs.ProposalPOLRound).TwoThirdsMajority(); ok {
					stateCh.Send(MsgToProto(&VoteSetMaj23Message{
						Height:  prs.Height,
						Round:   prs.ProposalPOLRound,
						Type:    tmproto.PrevoteType,
						BlockID: maj23,
					}), ps.peerID)
				}
			}
			// maybe send Height/Round/Precommits
			if maj23, ok := rs.Votes.Precommits(prs.Round).TwoThirdsMajority(); ok {
				stateCh.Send(MsgToProto(&VoteSetMaj23Message{
					Height:  prs.Height,
					Round:   prs.Round,
					Type:    tmproto.PrecommitType,
					BlockID: maj23,
				}), ps.peerID)
			}
		}

		// Little point sending LastCommitRound/LastCommit, these are fleeting and
		// non-blocking.
		if prs.CatchupCommitRound != -1 && prs.Height > 0 {
			if prs.Height <= r.state.blockStore.Height() && prs.Height >= r.state.blockStore.Base() {
				// maybe send Height/CatchupCommitRound/CatchupCommit
				if commit := r.state.LoadCommit(prs.Height); commit != nil {
					stateCh.Send(MsgToProto(&VoteSetMaj23Message{
						Height:  prs.Height,
						Round:   commit.Round,
						Type:    tmproto.PrecommitType,
						BlockID: commit.BlockID,
					}), ps.peerID)
				}
			}
		}
		if err := utils.Sleep(ctx, r.state.config.PeerQueryMaj23SleepDuration); err != nil {
			return err
		}
	}
}

// handleStateMessage handles envelopes sent from peers on the StateChannel.
// An error is returned if the message is unrecognized or if validation fails.
// If we fail to find the peer state for the envelope sender, we perform a no-op
// and return. This can happen when we process the envelope after the peer is
// removed.
func (r *Reactor) handleStateMessage(m p2p.RecvMsg[*tmcons.Message]) (err error) {
	defer r.recoverToErr(&err)
	ps, ok := r.GetPeerState(m.From)
	if !ok {
		r.logger.Debug("failed to find peer state", "peer", m.From, "ch_id", "StateChannel")
		return nil
	}

	msg, err := MsgFromProto(m.Message)
	if err != nil {
		return err
	}
	switch msg := msg.(type) {
	case *NewRoundStepMessage:
		r.state.mtx.RLock()
		initialHeight := r.state.state.InitialHeight
		r.state.mtx.RUnlock()

		if err := msg.ValidateHeight(initialHeight); err != nil {
			return err
		}
		ps.ApplyNewRoundStepMessage(msg)

	case *NewValidBlockMessage:
		ps.ApplyNewValidBlockMessage(msg)

	case *HasVoteMessage:
		if err := ps.ApplyHasVoteMessage(msg); err != nil {
			return fmt.Errorf("ps.ApplyHasVoteMessage(): %w", err)
		}
	case *VoteSetMaj23Message:
		r.state.mtx.RLock()
		height := r.state.roundState.Height()
		votes := r.state.roundState.Votes()
		r.state.mtx.RUnlock()

		if height != msg.Height {
			return nil
		}

		// peer claims to have a maj23 for some BlockID at <H,R,S>
		if err := votes.SetPeerMaj23(msg.Round, msg.Type, ps.peerID, msg.BlockID); err != nil {
			return err
		}

		// Respond with a VoteSetBitsMessage showing which votes we have and
		// consequently shows which we don't have.
		var ourVotes *bits.BitArray
		switch msg.Type {
		case tmproto.PrevoteType:
			ourVotes = votes.Prevotes(msg.Round).BitArrayByBlockID(msg.BlockID)
		case tmproto.PrecommitType:
			ourVotes = votes.Precommits(msg.Round).BitArrayByBlockID(msg.BlockID)
		default:
			panic("bad VoteSetBitsMessage field type; forgot to add a check in ValidateBasic?")
		}
		r.channels.votSet.Send(MsgToProto(&VoteSetBitsMessage{
			Height:  msg.Height,
			Round:   msg.Round,
			Type:    msg.Type,
			BlockID: msg.BlockID,
			Votes:   ourVotes,
		}), m.From)
	default:
		return fmt.Errorf("received unknown message on StateChannel: %T", msg)
	}

	return nil
}

// handleDataMessage handles envelopes sent from peers on the DataChannel. If we
// fail to find the peer state for the envelope sender, we perform a no-op and
// return. This can happen when we process the envelope after the peer is
// removed.
func (r *Reactor) handleDataMessage(ctx context.Context, m p2p.RecvMsg[*tmcons.Message]) (err error) {
	defer r.recoverToErr(&err)
	logger := r.logger.With("peer", m.From, "ch_id", "DataChannel")

	ps, ok := r.GetPeerState(m.From)
	if !ok || ps == nil {
		r.logger.Debug("failed to find peer state")
		return nil
	}

	if r.WaitSync() {
		logger.Debug("ignoring message received during sync", "msg", fmt.Sprintf("%T", m.Message))
		return nil
	}

	msg, err := MsgFromProto(m.Message)
	if err != nil {
		return err
	}
	switch msg := msg.(type) {
	case *ProposalMessage:
		ps.SetHasProposal(msg.Proposal)
		return utils.Send(ctx, r.state.peerMsgQueue, msgInfo{msg, m.From, tmtime.Now()})
	case *ProposalPOLMessage:
		ps.ApplyProposalPOLMessage(msg)
		return nil
	case *BlockPartMessage:
		ps.SetHasProposalBlockPart(msg.Height, msg.Round, int(msg.Part.Index))
		r.Metrics.BlockParts.With("peer_id", string(m.From)).Add(1)
		return utils.Send(ctx, r.state.peerMsgQueue, msgInfo{msg, m.From, tmtime.Now()})
	default:
		return fmt.Errorf("received unknown message on DataChannel: %T", msg)
	}
}

// handleVoteMessage handles envelopes sent from peers on the VoteChannel. If we
// fail to find the peer state for the envelope sender, we perform a no-op and
// return. This can happen when we process the envelope after the peer is
// removed.
func (r *Reactor) handleVoteMessage(ctx context.Context, m p2p.RecvMsg[*tmcons.Message]) (err error) {
	defer r.recoverToErr(&err)
	logger := r.logger.With("peer", m.From, "ch_id", "VoteChannel")

	ps, ok := r.GetPeerState(m.From)
	if !ok {
		r.logger.Debug("failed to find peer state")
		return nil
	}

	if r.WaitSync() {
		logger.Debug("ignoring message received during sync", "msg", fmt.Sprintf("%T", m.Message))
		return nil
	}

	msg, err := MsgFromProto(m.Message)
	if err != nil {
		return err
	}

	switch msg := msg.(type) {
	case *VoteMessage:
		r.state.mtx.RLock()
		height := r.state.roundState.Height()
		valSize := r.state.roundState.Validators().Size()
		lastCommitSize := r.state.roundState.LastCommit().Size()
		r.state.mtx.RUnlock()

		ps.EnsureVoteBitArrays(height, valSize)
		ps.EnsureVoteBitArrays(height-1, lastCommitSize)
		if err := ps.SetHasVote(msg.Vote); err != nil {
			return err
		}
		return utils.Send(ctx, r.state.peerMsgQueue, msgInfo{msg, m.From, tmtime.Now()})
	default:
		return fmt.Errorf("received unknown message on VoteChannel: %T", msg)
	}
}

// handleVoteSetBitsMessage handles envelopes sent from peers on the
// VoteSetBitsChannel. If we fail to find the peer state for the envelope sender,
// we perform a no-op and return. This can happen when we process the envelope
// after the peer is removed.
func (r *Reactor) handleVoteSetBitsMessage(m p2p.RecvMsg[*tmcons.Message]) (err error) {
	defer r.recoverToErr(&err)
	logger := r.logger.With("peer", m.From, "ch_id", "VoteSetBitsChannel")

	ps, ok := r.GetPeerState(m.From)
	if !ok || ps == nil {
		r.logger.Debug("failed to find peer state")
		return nil
	}

	if r.WaitSync() {
		logger.Debug("ignoring message received during sync", "msg", fmt.Sprintf("%T", m.Message))
		return nil
	}

	msg, err := MsgFromProto(m.Message)
	if err != nil {
		return err
	}
	switch msg := msg.(type) {
	case *VoteSetBitsMessage:
		r.state.mtx.RLock()
		height := r.state.roundState.Height()
		votes := r.state.roundState.Votes()
		r.state.mtx.RUnlock()

		if height == msg.Height {
			var ourVotes *bits.BitArray

			switch msg.Type {
			case tmproto.PrevoteType:
				ourVotes = votes.Prevotes(msg.Round).BitArrayByBlockID(msg.BlockID)
			case tmproto.PrecommitType:
				ourVotes = votes.Precommits(msg.Round).BitArrayByBlockID(msg.BlockID)
			default:
				panic("bad VoteSetBitsMessage field type; forgot to add a check in ValidateBasic?")
			}

			ps.ApplyVoteSetBitsMessage(msg, ourVotes)
		} else {
			ps.ApplyVoteSetBitsMessage(msg, nil)
		}
		return nil
	default:
		return fmt.Errorf("received unknown message on VoteSetBitsChannel: %T", msg)
	}
}

func (r *Reactor) recoverToErr(err *error) {
	if e := recover(); e != nil {
		*err = fmt.Errorf("panic in processing message: %v", e)
		r.logger.Error(
			"recovering from processing message panic",
			"err", *err,
			"stack", string(debug.Stack()),
		)
	}
}

// processStateCh initiates a blocking process where we listen for and handle
// envelopes on the StateChannel. Any error encountered during message
// execution will result in a PeerError being sent on the StateChannel. When
// the reactor is stopped, we will catch the signal and close the p2p Channel
// gracefully.
func (r *Reactor) processStateCh(ctx context.Context) error {
	for ctx.Err() == nil {
		m, err := r.channels.state.Recv(ctx)
		if err != nil {
			return err
		}
		if err := r.handleStateMessage(m); err != nil && ctx.Err() == nil {
			r.router.Evict(m.From, fmt.Errorf("consensus.state: %w", err))
		}
	}
	return ctx.Err()
}

// processDataCh initiates a blocking process where we listen for and handle
// envelopes on the DataChannel. Any error encountered during message
// execution will result in a PeerError being sent on the DataChannel. When
// the reactor is stopped, we will catch the signal and close the p2p Channel
// gracefully.
func (r *Reactor) processDataCh(ctx context.Context) error {
	for ctx.Err() == nil {
		m, err := r.channels.data.Recv(ctx)
		if err != nil {
			return err
		}
		if err := r.handleDataMessage(ctx, m); err != nil && ctx.Err() == nil {
			r.router.Evict(m.From, fmt.Errorf("consensus.data: %w", err))
		}
	}
	return ctx.Err()
}

// processVoteCh initiates a blocking process where we listen for and handle
// envelopes on the VoteChannel. Any error encountered during message
// execution will result in a PeerError being sent on the VoteChannel. When
// the reactor is stopped, we will catch the signal and close the p2p Channel
// gracefully.
func (r *Reactor) processVoteCh(ctx context.Context) error {
	for ctx.Err() == nil {
		m, err := r.channels.vote.Recv(ctx)
		if err != nil {
			return err
		}
		if err := r.handleVoteMessage(ctx, m); err != nil && ctx.Err() == nil {
			r.router.Evict(m.From, fmt.Errorf("consensus.vote: %w", err))
		}
	}
	return ctx.Err()
}

// processVoteCh initiates a blocking process where we listen for and handle
// envelopes on the VoteSetBitsChannel. Any error encountered during message
// execution will result in a PeerError being sent on the VoteSetBitsChannel.
// When the reactor is stopped, we will catch the signal and close the p2p
// Channel gracefully.
func (r *Reactor) processVoteSetBitsCh(ctx context.Context) error {
	for ctx.Err() == nil {
		m, err := r.channels.votSet.Recv(ctx)
		if err != nil {
			return err
		}
		if err := r.handleVoteSetBitsMessage(m); err != nil && ctx.Err() == nil {
			r.router.Evict(m.From, fmt.Errorf("consensus.voteSet: %w", err))
		}
	}
	return ctx.Err()
}

// processPeerUpdates initiates a blocking process where we listen for and handle
// PeerUpdate messages. When the reactor is stopped, we will catch the signal and
// close the p2p PeerUpdatesCh gracefully.
func (r *Reactor) processPeerUpdates(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		recv := r.router.Subscribe()
		for ctx.Err() == nil {
			update, err := recv.Recv(ctx)
			if err != nil {
				return err
			}
			switch update.Status {
			case p2p.PeerStatusUp:
				for peers := range r.peers.Lock() {
					if _, ok := peers[update.NodeID]; ok {
						continue
					}
					ps := NewPeerState(r.logger, update.NodeID)
					peerCtx, cancel := context.WithCancel(ctx)
					ps.cancel = cancel
					peers[update.NodeID] = ps
					s.Spawn(func() error {
						defer cancel()
						// Only ctx.Canceled is expected and only once peerCtx is done.
						if err := r.runPeer(peerCtx, ps); utils.IgnoreCancel(err) != nil || peerCtx.Err() == nil {
							return err
						}
						return nil
					})
				}
			case p2p.PeerStatusDown:
				for peers := range r.peers.Lock() {
					if ps, ok := peers[update.NodeID]; ok {
						ps.cancel()
						delete(peers, update.NodeID)
					}
				}
			}
		}
		return ctx.Err()
	})
}

func (r *Reactor) runPeer(ctx context.Context, ps *PeerState) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return r.gossipDataRoutine(ctx, ps) })
		s.Spawn(func() error { return r.gossipVotesRoutine(ctx, ps) })
		s.Spawn(func() error { return r.queryMaj23Routine(ctx, ps) })
		r.sendNewRoundStepMessage(ps.peerID)
		return nil
	})
}

func (r *Reactor) recordPeerMsg(msg msgInfo) {
	if ps, ok := r.GetPeerState(msg.PeerID); ok {
		switch msg.Msg.(type) {
		case *VoteMessage:
			ps.RecordVote()
		case *BlockPartMessage:
			ps.RecordBlockPart()
		}
	}
}

func (r *Reactor) SetStateSyncingMetrics(v float64) {
	r.Metrics.StateSyncing.Set(v)
}

func (r *Reactor) SetBlockSyncingMetrics(v float64) {
	r.Metrics.BlockSyncing.Set(v)
}

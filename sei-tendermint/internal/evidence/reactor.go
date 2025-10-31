package evidence

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	clist "github.com/tendermint/tendermint/internal/libs/clist"
	"github.com/tendermint/tendermint/internal/p2p"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/service"
	"github.com/tendermint/tendermint/libs/utils"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/types"
)

var _ service.Service = (*Reactor)(nil)

const (
	EvidenceChannel = p2p.ChannelID(0x38)

	maxMsgSize = 4194304 // 4MB TODO make it configurable

	// broadcast all uncommitted evidence this often. This sets when the reactor
	// goes back to the start of the list and begins sending the evidence again.
	// Most evidence should be committed in the very next block that is why we wait
	// just over the block production rate before sending evidence again.
	broadcastEvidenceIntervalS = 10
)

// GetChannelDescriptor produces an instance of a descriptor for this
// package's required channels.
func GetChannelDescriptor() p2p.ChannelDescriptor {
	return p2p.ChannelDescriptor{
		ID:                  EvidenceChannel,
		MessageType:         new(tmproto.Evidence),
		Priority:            6,
		RecvMessageCapacity: maxMsgSize,
		RecvBufferCapacity:  32,
		Name:                "evidence",
	}
}

// Reactor handles evpool evidence broadcasting amongst peers.
type Reactor struct {
	service.BaseService
	logger log.Logger

	evpool *Pool
	router *p2p.Router

	mtx sync.Mutex

	peerRoutines map[types.NodeID]context.CancelFunc
	channel      *p2p.Channel
}

// NewReactor returns a reference to a new evidence reactor, which implements the
// service.Service interface. It accepts a p2p Channel dedicated for handling
// envelopes with EvidenceList messages.
func NewReactor(
	logger log.Logger,
	router *p2p.Router,
	evpool *Pool,
) (*Reactor, error) {
	channel, err := router.OpenChannel(GetChannelDescriptor())
	if err != nil {
		return nil, fmt.Errorf("router.OpenChannel(): %w", err)
	}
	r := &Reactor{
		logger:       logger,
		evpool:       evpool,
		router:       router,
		channel:      channel,
		peerRoutines: make(map[types.NodeID]context.CancelFunc),
	}

	r.BaseService = *service.NewBaseService(logger, "Evidence", r)

	return r, nil
}

// OnStart starts separate go routines for each p2p Channel and listens for
// envelopes on each. In addition, it also listens for peer updates and handles
// messages on that p2p channel accordingly. The caller must be sure to execute
// OnStop to ensure the outbound p2p Channels are closed. No error is returned.
func (r *Reactor) OnStart(ctx context.Context) error {
	r.SpawnCritical("processEvidenceCh", func(ctx context.Context) error { return r.processEvidenceCh(ctx) })
	r.SpawnCritical("processPeerUpdates", func(ctx context.Context) error { return r.processPeerUpdates(ctx) })
	return nil
}

// OnStop stops the reactor by signaling to all spawned goroutines to exit and
// blocking until they all exit.
func (r *Reactor) OnStop() { r.evpool.Close() }

// handleEvidenceMessage handles envelopes sent from peers on the EvidenceChannel.
// It returns an error only if the Envelope.Message is unknown for this channel
// or if the given evidence is invalid. This should never be called outside of
// handleMessage.
func (r *Reactor) handleEvidenceMessage(ctx context.Context, m p2p.RecvMsg) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("panic in processing message: %v", e)
			r.logger.Error(
				"recovering from processing message panic",
				"err", err,
				"stack", string(debug.Stack()),
			)
		}
	}()
	switch msg := m.Message.(type) {
	case *tmproto.Evidence:
		// Process the evidence received from a peer
		// Evidence is sent and received one by one
		ev, err := types.EvidenceFromProto(msg)
		if err != nil {
			return fmt.Errorf("types.EvidenceFromProto(): %w", err)
		}
		if err := r.evpool.AddEvidence(ctx, ev); err != nil {
			// If we're given invalid evidence by the peer, notify the router that
			// we should remove this peer by returning an error.
			if _, ok := err.(*types.ErrInvalidEvidence); ok {
				return err
			}

		}

	default:
		return fmt.Errorf("received unknown message: %T", msg)
	}

	return nil
}

// processEvidenceCh implements a blocking event loop where we listen for p2p
// Envelope messages from the evidenceCh.
func (r *Reactor) processEvidenceCh(ctx context.Context) error {
	evidenceCh := r.channel
	for {
		m, err := evidenceCh.Recv(ctx)
		if err != nil {
			return err
		}
		if err := r.handleEvidenceMessage(ctx, m); err != nil {
			r.logger.Error("failed to process evidenceCh message", "err", err)
			r.router.PeerManager().SendError(p2p.PeerError{NodeID: m.From, Err: err})
		}
	}
}

// processPeerUpdate processes a PeerUpdate. For new or live peers it will check
// if an evidence broadcasting goroutine needs to be started. For down or
// removed peers, it will check if an evidence broadcasting goroutine
// exists and signal that it should exit.
//
// FIXME: The peer may be behind in which case it would simply ignore the
// evidence and treat it as invalid. This would cause the peer to disconnect.
// The peer may also receive the same piece of evidence multiple times if it
// connects/disconnects frequently from the broadcasting peer(s).
//
// REF: https://github.com/tendermint/tendermint/issues/4727
func (r *Reactor) processPeerUpdate(ctx context.Context, peerUpdate p2p.PeerUpdate) {
	evidenceCh := r.channel
	r.logger.Debug("received peer update", "peer", peerUpdate.NodeID, "status", peerUpdate.Status)

	r.mtx.Lock()
	defer r.mtx.Unlock()

	switch peerUpdate.Status {
	case p2p.PeerStatusUp:
		// Do not allow starting new evidence broadcast loops after reactor shutdown
		// has been initiated. This can happen after we've manually closed all
		// peer broadcast loops, but the router still sends in-flight peer updates.
		if !r.IsRunning() {
			return
		}

		// Check if we've already started a goroutine for this peer, if not we create
		// a new done channel so we can explicitly close the goroutine if the peer
		// is later removed, we increment the waitgroup so the reactor can stop
		// safely, and finally start the goroutine to broadcast evidence to that peer.
		_, ok := r.peerRoutines[peerUpdate.NodeID]
		if !ok {
			pctx, pcancel := context.WithCancel(ctx)
			r.peerRoutines[peerUpdate.NodeID] = pcancel
			go r.broadcastEvidenceLoop(pctx, peerUpdate.NodeID, evidenceCh)
		}

	case p2p.PeerStatusDown:
		// Check if we've started an evidence broadcasting goroutine for this peer.
		// If we have, we signal to terminate the goroutine via the channel's closure.
		// This will internally decrement the peer waitgroup and remove the peer
		// from the map of peer evidence broadcasting goroutines.
		closer, ok := r.peerRoutines[peerUpdate.NodeID]
		if ok {
			closer()
		}
	}
}

// processPeerUpdates initiates a blocking process where we listen for and handle
// PeerUpdate messages. When the reactor is stopped, we will catch the signal and
// close the p2p PeerUpdatesCh gracefully.
func (r *Reactor) processPeerUpdates(ctx context.Context) error {
	peerUpdates := r.router.PeerManager().Subscribe(ctx)
	for _, update := range peerUpdates.PreexistingPeers() {
		r.processPeerUpdate(ctx, update)
	}
	for {
		update, err := utils.Recv(ctx, peerUpdates.Updates())
		if err != nil {
			return err
		}
		r.processPeerUpdate(ctx, update)
	}
}

// broadcastEvidenceLoop starts a blocking process that continuously reads pieces
// of evidence off of a linked-list and sends the evidence in a p2p Envelope to
// the given peer by ID. This should be invoked in a goroutine per unique peer
// ID via an appropriate PeerUpdate. The goroutine can be signaled to gracefully
// exit by either explicitly closing the provided doneCh or by the reactor
// signaling to stop.
//
// TODO: This should be refactored so that we do not blindly gossip evidence
// that the peer has already received or may not be ready for.
//
// REF: https://github.com/tendermint/tendermint/issues/4727
func (r *Reactor) broadcastEvidenceLoop(ctx context.Context, peerID types.NodeID, evidenceCh *p2p.Channel) {
	var next *clist.CElement

	defer func() {
		r.mtx.Lock()
		delete(r.peerRoutines, peerID)
		r.mtx.Unlock()

		if e := recover(); e != nil {
			r.logger.Error(
				"recovering from broadcasting evidence loop",
				"err", e,
				"stack", string(debug.Stack()),
			)
		}
	}()

	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		// This happens because the CElement we were looking at got garbage
		// collected (removed). That is, .NextWaitChan() returned nil. So we can go
		// ahead and start from the beginning.
		if next == nil {
			select {
			case <-r.evpool.EvidenceWaitChan(): // wait until next evidence is available
				if next = r.evpool.EvidenceFront(); next == nil {
					continue
				}

			case <-ctx.Done():
				return
			}
		}

		ev := next.Value.(types.Evidence)
		evProto, err := types.EvidenceToProto(ev)
		if err != nil {
			panic(fmt.Errorf("failed to convert evidence: %w", err))
		}

		// Send the evidence to the corresponding peer. Note, the peer may be behind
		// and thus would not be able to process the evidence correctly. Also, the
		// peer may receive this piece of evidence multiple times if it added and
		// removed frequently from the broadcasting peer.

		evidenceCh.Send(evProto, peerID)
		r.logger.Debug("gossiped evidence to peer", "evidence", ev, "peer", peerID)

		select {
		case <-timer.C:
			// start from the beginning after broadcastEvidenceIntervalS seconds
			timer.Reset(time.Second * broadcastEvidenceIntervalS)
			next = nil

		case <-next.NextWaitChan():
			next = next.Next()
			timer.Stop()

		case <-ctx.Done():
			return
		}
	}
}

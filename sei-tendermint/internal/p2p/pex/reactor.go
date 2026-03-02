package pex

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/service"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"golang.org/x/time/rate"
)

var (
	_ service.Service = (*Reactor)(nil)
)

var maxPeerRecvRate = rate.Every(100 * time.Millisecond)

const (
	// PexChannel is a channel for PEX messages
	PexChannel = 0x00

	// over-estimate of max NetAddress size
	// hexID (40) + IP (16) + Port (2) + Name (100) ...
	// NOTE: dont use massive DNS name ..
	maxAddressSize = 256

	// NOTE: amplification factor!
	// small request results in up to maxMsgSize response
	maxMsgSize = 1000 + maxAddressSize*p2p.MaxPexAddrs

	// the minimum time one peer can send another request to the same peer
	maxPeerRecvBurst    = 10
	DefaultSendInterval = 10 * time.Second
)

var ErrNoPeersAvailable = errors.New("no available peers to send a PEX request to (retrying)")

// TODO: We should decide whether we want channel descriptors to be housed
// within each reactor (as they are now) or, considering that the reactor doesn't
// really need to care about the channel descriptors, if they should be housed
// in the node module.
func ChannelDescriptor() p2p.ChannelDescriptor[*pb.PexMessage] {
	return p2p.ChannelDescriptor[*pb.PexMessage]{
		ID:                  PexChannel,
		MessageType:         new(pb.PexMessage),
		Priority:            1,
		SendQueueCapacity:   10,
		RecvMessageCapacity: maxMsgSize,
		RecvBufferCapacity:  128,
		Name:                "pex",
	}
}

// The peer exchange or PEX reactor supports the peer manager by sending
// requests to other peers for addresses that can be given to the peer manager
// and at the same time advertises addresses to peers that need more.
type Reactor struct {
	service.BaseService
	sendInterval time.Duration
	logger       log.Logger
	router       *p2p.Router
	// peerLimiters limits the number of messages received from peers.
	peerLimiters utils.Mutex[map[types.NodeID]*rate.Limiter]
	channel      *p2p.Channel[*pb.PexMessage]
}

// NewReactor returns a reference to a new reactor.
func NewReactor(
	logger log.Logger,
	router *p2p.Router,
	sendInterval time.Duration,
) (*Reactor, error) {
	channel, err := p2p.OpenChannel(router, ChannelDescriptor())
	if err != nil {
		return nil, err
	}
	r := &Reactor{
		logger:       logger,
		sendInterval: sendInterval,
		channel:      channel,
		router:       router,
		peerLimiters: utils.NewMutex(map[types.NodeID]*rate.Limiter{}),
	}
	r.BaseService = *service.NewBaseService(logger, "PEX", r)
	return r, nil
}

func (r *Reactor) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnNamed("sendRoutine", func() error { return r.sendRoutine(ctx) })
		s.SpawnNamed("processPexCh", func() error { return r.processPexCh(ctx) })
		s.SpawnNamed("processPeerUpdates", func() error { return r.processPeerUpdates(ctx) })
		return nil
	})
}

// OnStart implements service.Service.
func (r *Reactor) OnStart(ctx context.Context) error {
	r.SpawnCritical("Run", func(ctx context.Context) error { return r.Run(ctx) })
	return nil
}

// OnStop implements service.Service.
func (r *Reactor) OnStop() {}

func wrap[T *pb.PexRequest | *pb.PexResponse](msg T) *pb.PexMessage {
	switch msg := any(msg).(type) {
	case *pb.PexRequest:
		return &pb.PexMessage{Sum: &pb.PexMessage_PexRequest{PexRequest: msg}}
	case *pb.PexResponse:
		return &pb.PexMessage{Sum: &pb.PexMessage_PexResponse{PexResponse: msg}}
	default:
		panic("unreachable")
	}
}

func (r *Reactor) sendRoutine(ctx context.Context) error {
	for {
		r.logger.Info("PEX broadcast")
		r.channel.Broadcast(wrap(&pb.PexRequest{}))
		if err := utils.Sleep(ctx, r.sendInterval); err != nil {
			return err
		}
	}
}

// processPexCh implements a blocking event loop where we listen for p2p
// Envelope messages from the pexCh.
func (r *Reactor) processPexCh(ctx context.Context) error {
	for ctx.Err() == nil {
		m, err := r.channel.Recv(ctx)
		if err != nil {
			return err
		}
		if err := r.handlePexMessage(m); err != nil {
			r.router.Evict(m.From, fmt.Errorf("pex: %w", err))
		}
	}
	return nil
}

// processPeerUpdates processes peer updates.
func (r *Reactor) processPeerUpdates(ctx context.Context) error {
	recv := r.router.Subscribe()
	for {
		update, err := recv.Recv(ctx)
		if err != nil {
			return err
		}
		switch update.Status {
		case p2p.PeerStatusDown:
			// TODO(gprusak): peer updates and pexChannel are not synchronized, therefore we
			// might have race conditions where we receive messages from peers which are already marked as down.
			for peerLimiters := range r.peerLimiters.Lock() {
				delete(peerLimiters, update.NodeID)
			}
		}
	}
}

// handlePexMessage handles envelopes sent from peers on the PexChannel.
// If an update was received, a new polling interval is returned; otherwise the
// duration is 0.
func (r *Reactor) handlePexMessage(m p2p.RecvMsg[*pb.PexMessage]) error {
	for peerLimiters := range r.peerLimiters.Lock() {
		if _, ok := peerLimiters[m.From]; !ok {
			peerLimiters[m.From] = rate.NewLimiter(maxPeerRecvRate, maxPeerRecvBurst)
		}
		if !peerLimiters[m.From].Allow() {
			return fmt.Errorf("peer rate limit exceeded")
		}
	}
	switch msg := m.Message.Sum.(type) {
	case *pb.PexMessage_PexRequest:
		// Fetch peers from the peer manager, convert NodeAddresses into URL
		// strings, and send them back to the caller.
		nodeAddresses := r.router.Advertise(p2p.MaxPexAddrs)
		pexAddresses := make([]*pb.PexAddress, len(nodeAddresses))
		for idx, addr := range nodeAddresses {
			pexAddresses[idx] = &pb.PexAddress{Url: addr.String()}
		}
		r.channel.Send(wrap(&pb.PexResponse{Addresses: pexAddresses}), m.From)
		return nil

	case *pb.PexMessage_PexResponse:
		resp := msg.PexResponse
		// Verify that the response does not exceed the safety limit.
		if got, wantMax := len(resp.Addresses), p2p.MaxPexAddrs; got > wantMax {
			return fmt.Errorf("peer sent too many addresses (%d > maxiumum %d)", got, wantMax)
		}

		var addrs []p2p.NodeAddress
		for _, pexAddress := range resp.Addresses {
			addr, err := p2p.ParseNodeAddress(pexAddress.Url)
			if err != nil {
				return fmt.Errorf("PEX parse node address error: %w", err)
			}
			addrs = append(addrs, addr)
		}
		if err := r.router.AddAddrs(addrs); err != nil {
			return fmt.Errorf("failed adding addresses from PEX response: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("received unknown message: %T", msg)
	}
}

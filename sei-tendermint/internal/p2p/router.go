package p2p

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/netip"
	"runtime"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/service"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/types"
)

const queueBufferDefault = 1024

// RouterOptions specifies options for a Router.
type RouterOptions struct {
	// ResolveTimeout is the timeout for resolving NodeAddress URLs.
	// 0 means no timeout.
	ResolveTimeout time.Duration

	// DialTimeout is the timeout for dialing a peer. 0 means no timeout.
	DialTimeout time.Duration

	// HandshakeTimeout is the timeout for handshaking with a peer. 0 means
	// no timeout.
	HandshakeTimeout time.Duration

	// MaxIncomingConnectionAttempts rate limits the number of incoming connection
	// attempts per IP address. Defaults to 100.
	MaxIncomingConnectionAttempts uint

	// IncomingConnectionWindow describes how often an IP address
	// can attempt to create a new connection. Defaults to 10
	// milliseconds, and cannot be less than 1 millisecond.
	IncomingConnectionWindow time.Duration

	// FilterPeerByIP is used by the router to inject filtering
	// behavior for new incoming connections. The router passes
	// the remote IP of the incoming connection the port number as
	// arguments. Functions should return an error to reject the
	// peer.
	FilterPeerByIP func(context.Context, netip.AddrPort) error

	// FilterPeerByID is used by the router to inject filtering
	// behavior for new incoming connections. The router passes
	// the NodeID of the node before completing the connection,
	// but this occurs after the handshake is complete. Filter by
	// IP address to filter before the handshake. Functions should
	// return an error to reject the peer.
	FilterPeerByID func(context.Context, types.NodeID) error

	// DialSleep controls the amount of time that the router
	// sleeps between dialing peers. If not set, a default value
	// is used that sleeps for a (random) amount of time up to 3
	// seconds between submitting each peer to be dialed.
	DialSleep func(context.Context) error

	// NumConcrruentDials controls how many parallel go routines
	// are used to dial peers. This defaults to the value of
	// runtime.NumCPU.
	NumConcurrentDials func() int
}

// Validate validates router options.
func (o *RouterOptions) Validate() error {
	switch {
	case o.IncomingConnectionWindow == 0:
		o.IncomingConnectionWindow = 100 * time.Millisecond
	case o.IncomingConnectionWindow < time.Millisecond:
		return fmt.Errorf("incomming connection window must be grater than 1m [%s]",
			o.IncomingConnectionWindow)
	}

	if o.MaxIncomingConnectionAttempts == 0 {
		o.MaxIncomingConnectionAttempts = 100
	}

	return nil
}

type peerState struct {
	cancel   context.CancelFunc
	queue    *Queue       // outbound messages per peer for all channels
	channels ChannelIDSet // the channels that the peer queue has open
}

// Router manages peer connections and routes messages between peers and reactor
// channels. It takes a PeerManager for peer lifecycle management (e.g. which
// peers to dial and when) and a set of Transports for connecting and
// communicating with peers.
//
// On startup, three main goroutines are spawned to maintain peer connections:
//
//	dialPeers(): in a loop, calls PeerManager.DialNext() to get the next peer
//	address to dial and spawns a goroutine that dials the peer, handshakes
//	with it, and begins to route messages if successful.
//
//	acceptPeers(): in a loop, waits for an inbound connection via
//	Transport.Accept() and spawns a goroutine that handshakes with it and
//	begins to route messages if successful.
//
//	evictPeers(): in a loop, calls PeerManager.EvictNext() to get the next
//	peer to evict, and disconnects it by closing its message queue.
//
// When a peer is connected, an outbound peer message queue is registered in
// peerQueues, and routePeer() is called to spawn off two additional goroutines:
//
//	sendPeer(): waits for an outbound message from the peerQueues queue,
//	marshals it, and passes it to the peer transport which delivers it.
//
//	receivePeer(): waits for an inbound message from the peer transport,
//	unmarshals it, and passes it to the appropriate inbound channel queue
//	in channelQueues.
//
// When a reactor opens a channel via OpenChannel, an inbound channel message
// queue is registered in channelQueues, and a channel goroutine is spawned:
//
//	routeChannel(): waits for an outbound message from the channel, looks
//	up the recipient peer's outbound message queue in peerQueues, and submits
//	the message to it.
//
// All channel sends in the router are blocking. It is the responsibility of the
// queue interface in peerQueues and channelQueues to prioritize and drop
// messages as appropriate during contention to prevent stalls and ensure good
// quality of service.
type Router struct {
	*service.BaseService
	logger log.Logger

	metrics *Metrics
	lc      *metricsLabelCache

	options     RouterOptions
	privKey     crypto.PrivKey
	peerManager *PeerManager
	chDescs     []*ChannelDescriptor
	transport   Transport
	connTracker connectionTracker

	peerStates       utils.RWMutex[map[types.NodeID]*peerState]
	nodeInfoProducer func() *types.NodeInfo

	// FIXME: We don't strictly need to use a mutex for this if we seal the
	// channels on router start. This depends on whether we want to allow
	// dynamic channels in the future.
	channelMtx      sync.RWMutex
	channelQueues   map[ChannelID]*Queue // inbound messages from all peers to a single channel
	channelMessages map[ChannelID]proto.Message

	chDescsToBeAdded []chDescAdderWithCallback

	dynamicIDFilterer func(context.Context, types.NodeID) error
}

type chDescAdderWithCallback struct {
	chDesc *ChannelDescriptor
	cb     func(*Channel)
}

// NewRouter creates a new Router. The given Transports must already be
// listening on appropriate interfaces, and will be closed by the Router when it
// stops.
func NewRouter(
	logger log.Logger,
	metrics *Metrics,
	privKey crypto.PrivKey,
	peerManager *PeerManager,
	nodeInfoProducer func() *types.NodeInfo,
	transport Transport,
	dynamicIDFilterer func(context.Context, types.NodeID) error,
	options RouterOptions,
) (*Router, error) {

	if err := options.Validate(); err != nil {
		return nil, err
	}

	router := &Router{
		logger:           logger,
		metrics:          metrics,
		lc:               newMetricsLabelCache(),
		privKey:          privKey,
		nodeInfoProducer: nodeInfoProducer,
		connTracker: newConnTracker(
			options.MaxIncomingConnectionAttempts,
			options.IncomingConnectionWindow,
		),
		chDescs:           make([]*ChannelDescriptor, 0),
		transport:         transport,
		peerManager:       peerManager,
		options:           options,
		channelQueues:     map[ChannelID]*Queue{},
		channelMessages:   map[ChannelID]proto.Message{},
		peerStates:        utils.NewRWMutex(map[types.NodeID]*peerState{}),
		dynamicIDFilterer: dynamicIDFilterer,
	}

	router.BaseService = service.NewBaseService(logger, "router", router)

	return router, nil
}

// ChannelCreator allows routers to construct their own channels,
// either by receiving a reference to Router.OpenChannel or using some
// kind shim for testing purposes.
type ChannelCreator func(context.Context, *ChannelDescriptor) (*Channel, error)

// OpenChannel opens a new channel for the given message type.
func (r *Router) OpenChannel(chDesc *ChannelDescriptor) (*Channel, error) {
	r.channelMtx.Lock()
	defer r.channelMtx.Unlock()

	id := chDesc.ID
	if _, ok := r.channelQueues[id]; ok {
		return nil, fmt.Errorf("channel %v already exists", id)
	}
	r.chDescs = append(r.chDescs, chDesc)

	messageType := chDesc.MessageType

	// TODO(gprusak): get rid of this random cap*cap value once we understand
	// what the sizes per channel really should be.
	queue := NewQueue(chDesc.RecvBufferCapacity * chDesc.RecvBufferCapacity)
	outCh := make(chan Envelope, chDesc.RecvBufferCapacity)
	errCh := make(chan PeerError, chDesc.RecvBufferCapacity)
	channel := NewChannel(id, queue, outCh, errCh)
	channel.name = chDesc.Name

	var wrapper Wrapper
	if w, ok := messageType.(Wrapper); ok {
		wrapper = w
	}

	r.channelQueues[id] = queue
	r.channelMessages[id] = messageType

	// add the channel to the nodeInfo if it's not already there.
	r.nodeInfoProducer().AddChannel(uint16(chDesc.ID))

	r.transport.AddChannelDescriptors([]*ChannelDescriptor{chDesc})

	r.Spawn("channel", func(ctx context.Context) error {
		return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			s.Spawn(func() error { return r.routeChannel(ctx, chDesc, outCh, wrapper) })
			for {
				peerError, err := utils.Recv(ctx, errCh)
				if err != nil {
					return err
				}
				shouldEvict := peerError.Fatal || r.peerManager.HasMaxPeerCapacity()
				r.logger.Error("peer error",
					"peer", peerError.NodeID,
					"err", peerError.Err,
					"evicting", shouldEvict,
				)
				if shouldEvict {
					r.peerManager.Errored(peerError.NodeID, peerError.Err)
				} else {
					r.peerManager.processPeerEvent(ctx, PeerUpdate{
						NodeID: peerError.NodeID,
						Status: PeerStatusBad,
					})
				}
			}
		})
	})
	return channel, nil
}

// routeChannel receives outbound channel messages and routes them to the
// appropriate peer. It also receives peer errors and reports them to the peer
// manager. It returns when either the outbound channel or error channel is
// closed, or the Router is stopped. wrapper is an optional message wrapper
// for messages, see Wrapper for details.
func (r *Router) routeChannel(
	ctx context.Context,
	chDesc *ChannelDescriptor,
	outCh <-chan Envelope,
	wrapper Wrapper,
) error {
	for {
		envelope, err := utils.Recv(ctx, outCh)
		if err != nil {
			return err
		}
		if envelope.IsZero() {
			continue
		}

		// Mark the envelope with the channel ID to allow sendPeer() to pass
		// it on to Transport.SendMessage().
		envelope.ChannelID = chDesc.ID

		// wrap the message in a wrapper message, if requested
		if wrapper != nil {
			msg := utils.ProtoClone(wrapper)
			if err := msg.Wrap(envelope.Message); err != nil {
				r.logger.Error("failed to wrap message", "channel", chDesc.ID, "err", err)
				continue
			}

			envelope.Message = msg
		}

		// collect peer queues to pass the message via
		var queues []*Queue
		if envelope.Broadcast {
			for states := range r.peerStates.RLock() {
				queues = make([]*Queue, 0, len(states))
				for _, s := range states {
					if _, ok := s.channels[chDesc.ID]; ok {
						queues = append(queues, s.queue)
					}
				}
			}
		} else {
			ok := false
			var s *peerState
			for states := range r.peerStates.RLock() {
				s, ok = states[envelope.To]
			}
			if !ok {
				r.logger.Debug("dropping message for unconnected peer", "peer", envelope.To, "channel", chDesc.ID)
				continue
			}
			if _, contains := s.channels[chDesc.ID]; !contains {
				// reactor tried to send a message across a channel that the
				// peer doesn't have available. This is a known issue due to
				// how peer subscriptions work:
				// https://github.com/tendermint/tendermint/issues/6598
				continue
			}
			queues = []*Queue{s.queue}
		}
		// send message to peers
		for _, q := range queues {
			if pruned, ok := q.Send(envelope, chDesc.Priority).Get(); ok {
				r.metrics.QueueDroppedMsgs.With("ch_id", fmt.Sprint(pruned.ChannelID), "direction", "out").Add(float64(1))
			}
		}
	}
}

func (r *Router) numConccurentDials() int {
	if r.options.NumConcurrentDials == nil {
		return runtime.NumCPU()
	}

	return r.options.NumConcurrentDials()
}

func (r *Router) filterPeersIP(ctx context.Context, addrPort netip.AddrPort) error {
	if r.options.FilterPeerByIP == nil {
		return nil
	}

	return r.options.FilterPeerByIP(ctx, addrPort)
}

func (r *Router) filterPeersID(ctx context.Context, id types.NodeID) error {
	// apply dynamic filterer first
	if r.dynamicIDFilterer != nil {
		if err := r.dynamicIDFilterer(ctx, id); err != nil {
			return err
		}
	}

	if r.options.FilterPeerByID == nil {
		return nil
	}

	return r.options.FilterPeerByID(ctx, id)
}

func (r *Router) dialSleep(ctx context.Context) error {
	if r.options.DialSleep != nil {
		return r.options.DialSleep(ctx)
	}
	const (
		maxDialerInterval = 3000
		minDialerInterval = 250
	)

	// nolint:gosec // G404: Use of weak random number generator
	dur := time.Duration(rand.Int63n(maxDialerInterval-minDialerInterval+1) + minDialerInterval)
	return utils.Sleep(ctx, dur*time.Millisecond)
}

// acceptPeers accepts inbound connections from peers on the given transport,
// and spawns goroutines that route messages to/from them.
func (r *Router) acceptPeers(ctx context.Context, transport Transport) error {
	for {
		conn, err := transport.Accept(ctx)
		if err != nil {
			return fmt.Errorf("failed to accept connection: %w", err)
		}
		r.metrics.NewConnections.With("direction", "in").Add(1)
		incomingAddr := conn.RemoteEndpoint().Addr
		if err := r.connTracker.AddConn(incomingAddr); err != nil {
			closeErr := conn.Close()
			r.logger.Error("rate limiting incoming peer",
				"err", err,
				"addr", incomingAddr.String(),
				"close_err", closeErr,
			)

			continue
		}

		// Spawn a goroutine for the handshake, to avoid head-of-line blocking.
		r.Spawn("openConnection", func(ctx context.Context) error {
			defer conn.Close()
			return r.openConnection(ctx, conn)
		})
	}
}

func (r *Router) openConnection(ctx context.Context, conn Connection) error {
	defer conn.Close()
	incomingAddr := conn.RemoteEndpoint().Addr
	defer r.connTracker.RemoveConn(incomingAddr)

	if err := r.filterPeersIP(ctx, incomingAddr); err != nil {
		r.logger.Debug("peer filtered by IP", "ip", incomingAddr, "err", err)
		return nil
	}

	// FIXME: The peer manager may reject the peer during Accepted()
	// after we've handshaked with the peer (to find out which peer it
	// is). However, because the handshake has no ack, the remote peer
	// will think the handshake was successful and start sending us
	// messages.
	//
	// This can cause problems in tests, where a disconnection can cause
	// the local node to immediately redial, while the remote node may
	// not have completed the disconnection yet and therefore reject the
	// reconnection attempt (since it thinks we're still connected from
	// before).
	//
	// The Router should do the handshake and have a final ack/fail
	// message to make sure both ends have accepted the connection, such
	// that it can be coordinated with the peer manager.
	peerInfo, err := r.handshakePeer(ctx, conn, "")
	if err != nil {
		return fmt.Errorf("peer handshake failed: endpoint=%v: %w", conn, err)
	}
	if err := r.filterPeersID(ctx, peerInfo.NodeID); err != nil {
		r.logger.Debug("peer filtered by node ID", "node", peerInfo.NodeID, "err", err)
		return nil
	}
	if err := r.peerManager.Accepted(peerInfo.NodeID); err != nil {
		return fmt.Errorf("failed to accept connection: op=incoming/accepted, peer=%v: %w", peerInfo.NodeID, err)
	}
	return r.routePeer(ctx, peerInfo.NodeID, conn, toChannelIDs(peerInfo.Channels))
}

// dialPeers maintains outbound connections to peers by dialing them.
func (r *Router) dialPeers(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		addresses := make(chan NodeAddress)
		// Start a limited number of goroutines to dial peers in
		// parallel. the goal is to avoid starting an unbounded number
		// of goroutines thereby spamming the network, but also being
		// able to add peers at a reasonable pace, though the number
		// is somewhat arbitrary. The action is further throttled by a
		// sleep after sending to the addresses channel.
		for range r.numConccurentDials() {
			s.Spawn(func() error {
				for {
					address, err := utils.Recv(ctx, addresses)
					if err != nil {
						return err
					}
					r.logger.Debug(fmt.Sprintf("Going to dial next peer %s", address.NodeID))
					r.connectPeer(ctx, address)
				}
			})
		}

		for {
			address, err := r.peerManager.DialNext(ctx)
			if err != nil {
				return fmt.Errorf("failed to find next peer to dial: %w", err)
			}
			if err := utils.Send(ctx, addresses, address); err != nil {
				return err
			}
			// this jitters the frequency that we call
			// DialNext and prevents us from attempting to
			// create connections too quickly.
			if err := r.dialSleep(ctx); err != nil {
				return err
			}
		}
	})
}

func (r *Router) connectPeer(ctx context.Context, address NodeAddress) {
	conn, err := r.dialPeer(ctx, address)
	switch {
	case errors.Is(err, context.Canceled):
		return
	case err != nil:
		r.logger.Debug("failed to dial peer", "peer", address, "err", err)
		if err = r.peerManager.DialFailed(ctx, address); err != nil {
			r.logger.Debug("failed to report dial failure", "peer", address, "err", err)
		}
		return
	}

	peerInfo, err := r.handshakePeer(ctx, conn, address.NodeID)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			r.logger.Debug("failed to handshake with peer", "peer", address, "err", err)
			if err := r.peerManager.DialFailed(ctx, address); err != nil {
				r.logger.Error("failed to report dial failure", "peer", address, "err", err)
			}
		}
		conn.Close()
		return
	}

	// TODO(gprusak): this symmetric logic for handling duplicate connections is a source of race conditions:
	// if 2 nodes try to establish a connection to each other at the same time, both connections will be dropped.
	// Instead either:
	// * break the symmetry by favoring incoming connection iff my.NodeID > peer.NodeID
	// * keep incoming and outcoming connection pools separate to avoid the collision (recommended)
	if err := r.peerManager.Dialed(address); err != nil {
		r.logger.Info("failed to dial peer", "op", "outgoing/dialing", "peer", address.NodeID, "err", err)
		conn.Close()
		return
	}

	r.Spawn("routePeer", func(ctx context.Context) error {
		defer conn.Close()
		return r.routePeer(ctx, address.NodeID, conn, toChannelIDs(peerInfo.Channels))
	})
}

// dialPeer connects to a peer by dialing it.
func (r *Router) dialPeer(ctx context.Context, address NodeAddress) (Connection, error) {
	resolveCtx := ctx
	if r.options.ResolveTimeout > 0 {
		var cancel context.CancelFunc
		resolveCtx, cancel = context.WithTimeout(resolveCtx, r.options.ResolveTimeout)
		defer cancel()
	}

	r.logger.Debug("dialing peer address", "peer", address)
	endpoints, err := address.Resolve(resolveCtx)
	switch {
	case err != nil:
		// Mark the peer as private so it's not broadcasted to other peers.
		// This is reset upon restart of the node.
		if r.peerManager.options.PrivatePeers != nil {
			r.peerManager.options.PrivatePeers[address.NodeID] = struct{}{}
		}
		return nil, fmt.Errorf("failed to resolve address %q: %w", address, err)
	case len(endpoints) == 0:
		return nil, fmt.Errorf("address %q did not resolve to any endpoints", address)
	}

	for _, endpoint := range endpoints {
		dialCtx := ctx
		if r.options.DialTimeout > 0 {
			var cancel context.CancelFunc
			dialCtx, cancel = context.WithTimeout(dialCtx, r.options.DialTimeout)
			defer cancel()
		}

		// FIXME: When we dial and handshake the peer, we should pass it
		// appropriate address(es) it can use to dial us back. It can't use our
		// remote endpoint, since TCP uses different port numbers for outbound
		// connections than it does for inbound. Also, we may need to vary this
		// by the peer's endpoint, since e.g. a peer on 192.168.0.0 can reach us
		// on a private address on this endpoint, but a peer on the public
		// Internet can't and needs a different public address.
		conn, err := r.transport.Dial(dialCtx, endpoint)
		if err != nil {
			r.logger.Debug("failed to dial endpoint", "peer", address.NodeID, "endpoint", endpoint, "err", err)
		} else {
			r.metrics.NewConnections.With("direction", "out").Add(1)
			r.logger.Debug("dialed peer", "peer", address.NodeID, "endpoint", endpoint)
			return conn, nil
		}
	}
	return nil, errors.New("all endpoints failed")
}

// handshakePeer handshakes with a peer, validating the peer's information. If
// expectID is given, we check that the peer's info matches it.
func (r *Router) handshakePeer(
	ctx context.Context,
	conn Connection,
	expectID types.NodeID,
) (types.NodeInfo, error) {

	if r.options.HandshakeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.options.HandshakeTimeout)
		defer cancel()
	}

	nodeInfo := r.nodeInfoProducer()
	peerInfo, err := conn.Handshake(ctx, *nodeInfo, r.privKey)
	if err != nil {
		return types.NodeInfo{}, err
	}
	if peerInfo.Network != nodeInfo.Network {
		if err := r.peerManager.Delete(peerInfo.NodeID); err != nil {
			return types.NodeInfo{}, fmt.Errorf("problem removing peer from store from incorrect network [%s]: %w", peerInfo.Network, err)
		}
		return types.NodeInfo{}, fmt.Errorf("connected to peer from wrong network, %q, removed from peer store", peerInfo.Network)
	}

	if expectID != "" && expectID != peerInfo.NodeID {
		return types.NodeInfo{}, fmt.Errorf("expected to connect with peer %q, got %q",
			expectID, peerInfo.NodeID)
	}

	if err := nodeInfo.CompatibleWith(peerInfo); err != nil {
		return types.NodeInfo{}, ErrRejected{
			err:            err,
			id:             peerInfo.ID(),
			isIncompatible: true,
		}
	}
	return peerInfo, nil
}

// routePeer routes inbound and outbound messages between a peer and the reactor
// channels. It will close the given connection and send queue when done, or if
// they are closed elsewhere it will cause this method to shut down and return.
func (r *Router) routePeer(ctx context.Context, peerID types.NodeID, conn Connection, channels ChannelIDSet) error {
	r.metrics.Peers.Add(1)
	r.peerManager.Ready(ctx, peerID, channels)
	peerCtx, cancel := context.WithCancel(ctx)
	state := &peerState{
		cancel:   cancel,
		queue:    NewQueue(queueBufferDefault),
		channels: channels,
	}
	for states := range r.peerStates.Lock() {
		if old, ok := states[peerID]; ok {
			old.cancel()
		}
		states[peerID] = state
	}
	r.logger.Debug("peer connected", "peer", peerID, "endpoint", conn)
	err := scope.Run(peerCtx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return r.receivePeer(ctx, peerID, conn) })
		s.Spawn(func() error { return r.sendPeer(ctx, peerID, conn, state.queue) })
		<-ctx.Done()
		// TODO(gprusak): we need to close the connection here, because
		// the mock connection used in tests does not respect the context.
		// Get rid of these stupid mocks.
		_ = conn.Close()
		return nil
	})
	r.logger.Info("peer disconnected", "peer", peerID, "endpoint", conn, "err", err)
	for states := range r.peerStates.Lock() {
		if states[peerID] == state {
			delete(states, peerID)
		}
	}
	// TODO(gprusak): investigate if peerManager handles overlapping connetions correctly
	r.peerManager.Disconnected(ctx, peerID)
	r.metrics.Peers.Add(-1)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// receivePeer receives inbound messages from a peer, deserializes them and
// passes them on to the appropriate channel.
func (r *Router) receivePeer(ctx context.Context, peerID types.NodeID, conn Connection) error {
	for {
		chID, bz, err := conn.ReceiveMessage(ctx)
		if err != nil {
			return err
		}

		r.channelMtx.RLock()
		queue, ok := r.channelQueues[chID]
		messageType := r.channelMessages[chID]
		r.channelMtx.RUnlock()

		if !ok {
			// TODO(gprusak): verify if this is a misbehavior, and drop the peer if it is.
			r.logger.Debug("dropping message for unknown channel", "peer", peerID, "channel", chID)
			continue
		}

		msg := proto.Clone(messageType)
		if err := proto.Unmarshal(bz, msg); err != nil {
			return fmt.Errorf("message decoding failed, dropping message: [peer=%v] %w", peerID, err)
		}

		if wrapper, ok := msg.(Wrapper); ok {
			msg, err = wrapper.Unwrap()
			if err != nil {
				return fmt.Errorf("failed to unwrap message: %w", err)
			}
		}

		// Priority is not used since all messages in this queue are from the same channel.
		if pruned, ok := queue.Send(Envelope{From: peerID, Message: msg, ChannelID: chID}, 0).Get(); ok {
			r.metrics.QueueDroppedMsgs.With("ch_id", fmt.Sprint(pruned.ChannelID), "direction", "in").Add(float64(1))
		}
		r.metrics.PeerReceiveBytesTotal.With(
			"chID", fmt.Sprint(chID),
			"peer_id", string(peerID),
			"message_type", r.lc.ValueToMetricLabel(msg)).Add(float64(proto.Size(msg)))
		r.logger.Debug("received message", "peer", peerID, "message", msg)
	}
}

// sendPeer sends queued messages to a peer.
func (r *Router) sendPeer(ctx context.Context, peerID types.NodeID, conn Connection, peerQueue *Queue) error {
	for {
		start := time.Now().UTC()
		envelope, err := peerQueue.Recv(ctx)
		if err != nil {
			return err
		}
		r.metrics.RouterPeerQueueRecv.Observe(time.Since(start).Seconds())
		if envelope.Message == nil {
			r.logger.Error("dropping nil message", "peer", peerID)
			continue
		}
		bz, err := proto.Marshal(envelope.Message)
		if err != nil {
			r.logger.Error("failed to marshal message", "peer", peerID, "err", err)
			continue
		}

		if err = conn.SendMessage(ctx, envelope.ChannelID, bz); err != nil {
			r.logger.Error("failed to send message", "peer", peerID, "err", err)
			return err
		}

		r.logger.Debug("sent message", "peer", envelope.To, "message", envelope.Message)
	}
}

// evictPeers evicts connected peers as requested by the peer manager.
func (r *Router) evictPeers(ctx context.Context) error {
	for {
		ev, err := r.peerManager.EvictNext(ctx)
		if err != nil {
			return fmt.Errorf("failed to find next peer to evict: %w", err)
		}
		for states := range r.peerStates.Lock() {
			if s, ok := states[ev.ID]; ok {
				r.logger.Info("evicting peer", "peer", ev.ID, "cause", ev.Cause)
				s.cancel()
			}
		}
	}
}

func (r *Router) AddChDescToBeAdded(chDesc *ChannelDescriptor, callback func(*Channel)) {
	r.chDescsToBeAdded = append(r.chDescsToBeAdded, chDescAdderWithCallback{
		chDesc: chDesc,
		cb:     callback,
	})
}

// OnStart implements service.Service.
func (r *Router) OnStart(ctx context.Context) error {
	for _, chDescWithCb := range r.chDescsToBeAdded {
		if ch, err := r.OpenChannel(chDescWithCb.chDesc); err != nil {
			return err
		} else {
			chDescWithCb.cb(ch)
		}
	}

	r.SpawnCritical("transport.Run", func(ctx context.Context) error {
		return r.transport.Run(ctx)
	})
	r.SpawnCritical("dialPeers", func(ctx context.Context) error { return r.dialPeers(ctx) })
	r.SpawnCritical("evictPeers", func(ctx context.Context) error { return r.evictPeers(ctx) })
	r.SpawnCritical("acceptPeers", func(ctx context.Context) error { return r.acceptPeers(ctx, r.transport) })
	return nil
}

// OnStop implements service.Service.
//
// All channels must be closed by OpenChannel() callers before stopping the
// router, to prevent blocked channel sends in reactors. Channels are not closed
// here, since that would cause any reactor senders to panic, so it is the
// sender's responsibility.
func (r *Router) OnStop() {}

type ChannelIDSet map[ChannelID]struct{}

func (cs ChannelIDSet) Contains(id ChannelID) bool {
	_, ok := cs[id]
	return ok
}

func toChannelIDs(bytes []byte) ChannelIDSet {
	c := make(map[ChannelID]struct{}, len(bytes))
	for _, b := range bytes {
		c[ChannelID(b)] = struct{}{}
	}
	return c
}

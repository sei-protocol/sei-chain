package p2p

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sync/semaphore"
	"math"
	"net"
	"net/netip"
	"time"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/service"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/im"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"github.com/tendermint/tendermint/types"
)

// RouterOptions specifies options for a Router.
type RouterOptions struct {
	// ResolveTimeout is the timeout for resolving NodeAddress URLs.
	// 0 means no timeout.
	ResolveTimeout utils.Option[time.Duration]

	// DialTimeout is the timeout for dialing a peer. 0 means no timeout.
	DialTimeout utils.Option[time.Duration]

	// HandshakeTimeout is the timeout for handshaking with a peer. 0 means
	// no timeout.
	HandshakeTimeout utils.Option[time.Duration]

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

	// MaxAcceptedConnections is the maximum number of simultaneous accepted
	// (incoming) connections. Beyond this, new connections will block until
	// a slot is free.
	MaxConnections utils.Option[int]

	Endpoint Endpoint

	Connection conn.MConnConfig
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

	conns            utils.AtomicWatch[im.Map[types.NodeID,*Connection]]
	nodeInfoProducer func() *types.NodeInfo

	channels utils.RWMutex[map[ChannelID]*channel]

	dynamicIDFilterer func(context.Context, types.NodeID) error

	started chan struct{}
}

func (r *Router) getChannelDescs() []*ChannelDescriptor {
	for channels := range r.channels.RLock() {
		descs := make([]*ChannelDescriptor, 0, len(channels))
		for _, ch := range channels {
			descs = append(descs, &ch.desc)
		}
		return descs
	}
	panic("unreachable")
}

// NewRouter creates a new Router.
func NewRouter(
	logger log.Logger,
	metrics *Metrics,
	privKey crypto.PrivKey,
	peerManager *PeerManager,
	nodeInfoProducer func() *types.NodeInfo,
	dynamicIDFilterer func(context.Context, types.NodeID) error,
	options RouterOptions,
) *Router {
	router := &Router{
		logger:           logger,
		metrics:          metrics,
		lc:               newMetricsLabelCache(),
		privKey:          privKey,
		nodeInfoProducer: nodeInfoProducer,

		peerManager:       peerManager,
		options:           options,
		channels:          utils.NewRWMutex(map[ChannelID]*channel{}),
		conns:             utils.NewAtomicWatch(im.NewMap[types.NodeID,*Connection]()),
		dynamicIDFilterer: dynamicIDFilterer,
		started: make(chan struct{}),
	}
	router.BaseService = service.NewBaseService(logger, "router", router)
	return router
}

func (r *Router) PeerManager() *PeerManager {
	return r.peerManager
}

// PeerRatio returns the ratio of peer addresses stored to the maximum size.
func (r *Router) PeerRatio() float64 {
	m,ok := r.options.MaxConnections.Get()
	if !ok || m == 0 {
		return 0
	}
	return float64(r.conns.Load().Len())/float64(m)
}

func (r *Router) Endpoint() Endpoint {
	return r.options.Endpoint
}

func (r *Router) WaitForStart(ctx context.Context) error {
	_, _, err := utils.RecvOrClosed(ctx, r.started)
	return err
}

func (r *Router) OpenChannelOrPanic(chDesc ChannelDescriptor) *Channel {
	ch, err := r.OpenChannel(chDesc)
	if err != nil {
		panic(err)
	}
	return ch
}

// OpenChannel opens a new channel for the given message type.
func (r *Router) OpenChannel(chDesc ChannelDescriptor) (*Channel, error) {
	for channels := range r.channels.Lock() {
		id := chDesc.ID
		if _, ok := channels[id]; ok {
			return nil, fmt.Errorf("channel %v already exists", id)
		}
		channels[id] = newChannel(chDesc)
		// add the channel to the nodeInfo if it's not already there.
		r.nodeInfoProducer().AddChannel(uint16(chDesc.ID))
		return &Channel{
			router:  r,
			channel: channels[id],
		}, nil
	}
	panic("unreachable")
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

func (r *Router) acceptPeers(ctx context.Context) error {
	if err := r.Endpoint().Validate(); err != nil {
		return err
	}
	listener, err := tcp.Listen(r.Endpoint().AddrPort)
	if err != nil {
		return fmt.Errorf("net.Listen(): %w", err)
	}
	close(r.started) // signal that we are listening

	sem := semaphore.NewWeighted(int64(r.options.MaxConnections.Or(math.MaxInt)))
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for {
			if err := sem.Acquire(ctx, 1); err != nil {
				return err
			}
			tcpConn, err := listener.AcceptOrClose(ctx)
			if err != nil {
				return err
			}

			// Spawn a goroutine per connection.
			s.Spawn(func() error {
				defer sem.Release(1)
				if err := r.openConnection(ctx, tcpConn); err != nil {
					r.logger.Error("accept", "err", err)
				}
				return nil
			})
		}
	})
}

func (r *Router) addConn(c *Connection) {
	r.metrics.Peers.Add(1)
	peerID := c.PeerInfo().NodeID
	type M = im.Map[types.NodeID,*Connection]
	r.conns.Update(func(conns M) (M,bool) {
		if old, ok := conns.Get(peerID); ok {
			old.Close()
		}
		return conns.Set(peerID, c),true
	})
}

func (r *Router) delConn(c *Connection) {
	r.metrics.Peers.Add(-1)
	peerID := c.PeerInfo().NodeID
	type M = im.Map[types.NodeID,*Connection]
	r.conns.Update(func(conns M) (M,bool) {
		if old, ok := conns.Get(peerID); ok && old == c {
			return conns.Delete(peerID),true
		}
		return M{},false
	})
}

func (r *Router) ConnectPeer(ctx context.Context, address NodeAddress) (c *Connection, err error) {
	tcpConn, err := r.Dial(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("r.Dial(): %w", err)
	}
	conn, err := r.handshakePeer(ctx, tcpConn, utils.Some(address.NodeID))
	if err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("failed to handshake with peer %v: %w", address, err)
	}
	return conn, nil
}

// dialPeer connects to a peer by dialing it.
func (r *Router) Dial(ctx context.Context, address NodeAddress) (*net.TCPConn, error) {
	resolveCtx := ctx
	if d,ok := r.options.ResolveTimeout.Get(); ok {
		var cancel context.CancelFunc
		resolveCtx, cancel = context.WithTimeout(resolveCtx, d)
		defer cancel()
	}

	r.logger.Debug("dialing peer address", "peer", address)
	endpoints, err := address.Resolve(resolveCtx)
	if err != nil {
		return nil, fmt.Errorf("address.Resolve(): %w",err)
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("address %q did not resolve to any endpoints", address)
	}

	for _, endpoint := range endpoints {
		dialCtx := ctx
		if d,ok := r.options.DialTimeout.Get(); ok {
			var cancel context.CancelFunc
			dialCtx, cancel = context.WithTimeout(dialCtx, d)
			defer cancel()
		}
		if err := endpoint.Validate(); err != nil {
			return nil, err
		}
		if endpoint.Port() == 0 {
			endpoint.AddrPort = netip.AddrPortFrom(endpoint.Addr(), 26657)
		}
		dialer := net.Dialer{}
		tcpConn, err := dialer.DialContext(dialCtx, "tcp", endpoint.String())
		if err != nil {
			r.logger.Debug("failed to dial endpoint", "peer", address.NodeID, "endpoint", endpoint, "err", err)
			continue
		}
		r.metrics.NewConnections.With("direction", "out").Add(1)
		r.logger.Debug("dialed peer", "peer", address.NodeID, "endpoint", endpoint)
		return tcpConn.(*net.TCPConn), nil
	}
	return nil, errors.New("all endpoints failed")
}

// handshakePeer handshakes with a peer, validating the peer's information. If
// expectID is given, we check that the peer's info matches it.
func (r *Router) handshakePeer(
	ctx context.Context,
	tcpConn *net.TCPConn,
	expectID utils.Option[types.NodeID],
) (c *Connection, err error) {
	defer func() {
		if err != nil {
			tcpConn.Close()
		}
	}()
	if d,ok := r.options.HandshakeTimeout.Get(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
	conn, err := HandshakeOrClose(ctx, r, tcpConn)
	if err != nil {
		return nil, err
	}
	peerInfo := conn.PeerInfo()
	nodeInfo := r.nodeInfoProducer()
	if peerInfo.Network != nodeInfo.Network {
		return nil, errBadNetwork{fmt.Errorf("connected to peer from wrong network, %q, removed from peer store", peerInfo.Network)}
	}
	if want, ok := expectID.Get(); ok && want != peerInfo.NodeID {
		return nil, fmt.Errorf("expected to connect with peer %q, got %q",
			want, peerInfo.NodeID)
	}
	if err := nodeInfo.CompatibleWith(peerInfo); err != nil {
		return nil, ErrRejected{
			err:            err,
			id:             peerInfo.ID(),
			isIncompatible: true,
		}
	}
	return conn, nil
}

func (r *Router) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnNamed("acceptPeers", func() error { return r.acceptPeers(ctx) })
		s.SpawnNamed("peerManager", func() error { return r.peerManager.Run(ctx, r) })
		return nil
	})
}

// OnStart implements service.Service.
func (r *Router) OnStart(ctx context.Context) error {
	r.SpawnCritical("Run", func(ctx context.Context) error { return r.Run(ctx) })
	return nil
}

// OnStop implements service.Service.
func (r *Router) OnStop() {}

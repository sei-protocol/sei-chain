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

	// MaxAcceptedConnections is the maximum number of simultaneous accepted
	// (incoming) connections. Beyond this, new connections will block until
	// a slot is free.
	MaxConnections utils.Option[int]

	Endpoint Endpoint

	Connection conn.MConnConfig

	PeerManager *PeerManagerOptions
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
	connTracker *connTracker
	privKey     crypto.PrivKey
	peerManager *PeerManager

	conns            utils.Watch[*connRegistry]
	nodeInfoProducer func() *types.NodeInfo

	channels utils.RWMutex[map[ChannelID]*channel]

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
	nodeInfoProducer func() *types.NodeInfo,
	options RouterOptions,
) *Router {
	peerManager,err := NewPeerManager(options.PeerManager)
	if err!=nil {
		// TODO: return error
		panic(fmt.Sprintf("failed to create PeerManager: %v", err))
	}
	router := &Router{
		logger:           logger,
		metrics:          metrics,
		lc:               newMetricsLabelCache(),
		privKey:          privKey,
		nodeInfoProducer: nodeInfoProducer,
		connTracker: newConnTracker(
			options.getMaxIncomingConnectionAttempts(),
			options.getIncomingConnectionWindow(),
		),
		peerManager:       peerManager,
		options:           options,
		channels:          utils.NewRWMutex(map[ChannelID]*channel{}),
		conns:             utils.NewAtomicWatch(newConnRegistry()),
		started: make(chan struct{}),
	}
	router.BaseService = service.NewBaseService(logger, "router", router)
	return router
}

// TODO: hide peer manager altogether.
func (r *Router) PeerManager() *PeerManager {
	return r.peerManager
}

// PeerRatio returns the ratio of peer addresses stored to the maximum size.
func (r *Router) PeerRatio() float64 {
	m,ok := r.options.MaxConnections.Get()
	if !ok || m == 0 {
		return 0
	}
	return float64(r.conns.Load().conns.Len())/float64(m)
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
				defer tcpConn.Close()
				defer sem.Release(1)
				remoteAddr := tcp.RemoteAddr(tcpConn)
				if err := r.connTracker.AddConn(remoteAddr); err != nil {
					r.logger.Error("rate limiting incoming", "addr",remoteAddr, "err",err)
					return nil
				}
				defer r.connTracker.RemoveConn(remoteAddr)
				conn, err := r.handshakePeer(ctx, tcpConn, utils.None[NodeAddress]())
				if err != nil {
					r.logger.Error("r.handshakePeer()", "addr", remoteAddr, "err", err)
					return nil
				}
				if err := conn.Run(ctx, r); err != nil {
					r.logger.Error("accept", "err", err)
				}
				return nil
			})
		}
	})
}

func (r *Router) dialPeers(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		limiter := rate.NewLimiter()
		s.Spawn(func() error {
			for {
				if err:=limiter.Wait(ctx); err!=nil { return err }
				addr,err := r.conns.PeerToDial(ctx)
				if err!=nil { return err }
				s.Spawn(func() error { r.DialAndRun(ctx, addr) })
			}
		})
		s.Spawn(func() error {
			for {
				if err:=limiter.Wait(ctx); err!=nil { return err }
				addr,err := r.conns.PersistentPeerToDial(ctx)
				if err!=nil { return err }
				s.Spawn(func() error { r.DialAndRun(ctx, addr) })
			}
		})
		return nil
	})
}

// SendError reports a peer misbehavior to the router.
func (r *Router) SendError(pe PeerError) {
	r.logger.Error("peer error",
		"peer", pe.NodeID,
		"err", pe.Err,
		"evicting", pe.Fatal,
	)
	if pe.Fatal && !r.peerManager.options.persistent(pe.NodeID) {
		r.logger.Info("evicting peer", "peer", pe.NodeID, "cause", pe.Err)
		r.peerManager.Drop(pe.NodeID)
	}
}

// Status returns the status for a peer, primarily for testing.
func (r *Router) Connected(id types.NodeID) bool {
	_,ok := r.conns.Load().conns.Get(id)
	return ok
}

func (r *Router) GetBlockSyncPeers() map[types.NodeID]bool {
	return r.peerManager.options.BlockSyncPeers
}

func (r *Router) ConnectPeer(ctx context.Context, addr NodeAddress) (c *Connection, err error) {
	conn, err := r.handshakePeer(ctx, tcpConn, utils.Some(addr))
	if err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("failed to handshake with peer %v: %w", addr, err)
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
	dialAddr utils.Option[NodeAddress],
) (c *Connection, err error) {
	defer func() { if err != nil { tcpConn.Close() } }()
	remoteAddr := tcp.RemoteAddr(tcpConn)
	if dialAddr.IsPresent() {
		r.metrics.NewConnections.With("direction", "out").Add(1)
	} else {
		r.metrics.NewConnections.With("direction", "in").Add(1)
	}

	if err := r.peerManager.options.filterPeersIP(ctx, remoteAddr); err != nil {
		return nil, fmt.Errorf("peer filtered by IP", "ip", remoteAddr, "err", err)
	}
	if d,ok := r.options.HandshakeTimeout.Get(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
	conn, err := r.HandshakeOrClose(ctx, tcpConn, dialAddr)
	if err != nil {
		return nil, err
	}
	peerInfo := conn.PeerInfo()
	nodeInfo := r.nodeInfoProducer()
	if peerInfo.Network != nodeInfo.Network {
		return nil, errBadNetwork{fmt.Errorf("connected to peer from wrong network, %q, removed from peer store", peerInfo.Network)}
	}
	if want, ok := dialAddr.Get(); ok && want.NodeID != peerInfo.NodeID {
		return nil, fmt.Errorf("expected to connect with peer %q, got %q",
			want.NodeID, peerInfo.NodeID)
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
		s.SpawnNamed("dialPeers", func() error { return r.dialPeers(ctx) })
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

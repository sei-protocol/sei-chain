package p2p

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sync/semaphore"
	"math/rand"
	"math"
	"net"
	"net/netip"
	"runtime"
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

	// MaxAcceptedConnections is the maximum number of simultaneous accepted
	// (incoming) connections. Beyond this, new connections will block until
	// a slot is free. 0 means unlimited.
	MaxAcceptedConnections uint32

	Endpoint Endpoint

	Connection conn.MConnConfig
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
	connTracker *connTracker

	conns       utils.RWMutex[map[types.NodeID]*Connection]
	nodeInfoProducer func() *types.NodeInfo

	channels         utils.RWMutex[map[ChannelID]*channel]

	dynamicIDFilterer func(context.Context, types.NodeID) error

	started  chan struct{}
}

func (r *Router) getChannelDescs() []*ChannelDescriptor {
	for channels := range r.channels.Lock() {
		descs := make([]*ChannelDescriptor, 0, len(channels))
		for _,ch := range channels {
			descs = append(descs,&ch.desc)
		}
		return descs
	}
	panic("unreachable")
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
		peerManager:       peerManager,
		options:           options,
		channels:          utils.NewRWMutex(map[ChannelID]*channel{}),
		conns:        utils.NewRWMutex(map[types.NodeID]*Connection{}),
		dynamicIDFilterer: dynamicIDFilterer,

		// This is rendezvous channel, so that no unclosed connections get stuck inside
		// when transport is closing.
		started:  make(chan struct{}),
	}

	router.BaseService = service.NewBaseService(logger, "router", router)

	return router, nil
}

func (r *Router) Endpoint() Endpoint {
	return r.options.Endpoint
}

func (r *Router) Address() NodeAddress {
	return r.Endpoint().NodeAddress(r.nodeInfoProducer().NodeID)
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
		return &Channel {
			router: r,
			channel: channels[id],
		}, nil
	}
	panic("unreachable")
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

func (r *Router) acceptPeers(ctx context.Context) error {
	if err := r.Endpoint().Validate(); err != nil {
		return err
	}
	var err error
	var listener net.Listener
	listener, err = tcp.Listen(r.Endpoint().AddrPort)
	if err != nil {
		return fmt.Errorf("net.Listen(): %w", err)
	}
	close(r.started) // signal that we are listening

	maxConns := r.options.MaxAcceptedConnections
	if maxConns == 0 {
		maxConns = math.MaxInt32
	}
	sem := semaphore.NewWeighted(int64(maxConns))

	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			<-ctx.Done()
			s.Cancel(ctx.Err())
			listener.Close()
			return nil
		})
		for {
			if err := sem.Acquire(ctx, 1); err!=nil {
				return err
			}
			tcpConn, err := listener.Accept()
			if err != nil {
				return err
			}

			// Spawn a goroutine per connection.
			s.Spawn(func() error {
				defer sem.Release(1)
				if err := r.openConnection(ctx, tcpConn.(*net.TCPConn)); err != nil {
					r.logger.Error("accept", "err", err)
				}
				return nil
			})
		}
	})
}

func (r *Router) openConnection(ctx context.Context, tcpConn *net.TCPConn) error {
	defer tcpConn.Close()
	r.metrics.NewConnections.With("direction", "in").Add(1)
	incomingAddr := remoteEndpoint(tcpConn).AddrPort
	if err := r.connTracker.AddConn(incomingAddr); err != nil {
		return fmt.Errorf("rate limiting incoming peer %v: %w",incomingAddr, err)
	}
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
	conn, err := r.handshakePeer(ctx, tcpConn, utils.None[types.NodeID]())
	if err != nil {
		return fmt.Errorf("peer handshake failed: endpoint=%v: %w", conn, err)
	}
	peerInfo := conn.PeerInfo()
	if err := r.filterPeersID(ctx, peerInfo.NodeID); err != nil {
		r.logger.Debug("peer filtered by node ID", "node", peerInfo.NodeID, "err", err)
		return nil
	}
	if err := r.peerManager.Accepted(peerInfo.NodeID); err != nil {
		return fmt.Errorf("failed to accept connection: op=incoming/accepted, peer=%v: %w", peerInfo.NodeID, err)
	}
	return conn.Run(ctx, r)
}

// dialPeers maintains outbound connections to peers by dialing them.
func (r *Router) dialPeers(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		sem := semaphore.NewWeighted(int64(r.numConccurentDials()))
		for {
			if err := sem.Acquire(ctx, 1); err != nil {
				return err
			}
			address, err := r.peerManager.DialNext(ctx)
			if err != nil {
				return fmt.Errorf("failed to find next peer to dial: %w", err)
			}
			s.Spawn(func() error {
				err := func() error {
					r.logger.Debug("Going to dial","peer", address.NodeID)
					conn,err := r.connectPeer(ctx, address)
					sem.Release(1)

					if err != nil {
						return fmt.Errorf("connectPeer(): %w", err)
					}
					return conn.Run(ctx, r)
				}()
				r.logger.Error("dial","err",err)
				return nil
			})

			// this jitters the frequency that we call
			// DialNext and prevents us from attempting to
			// create connections too quickly.
			if err := r.dialSleep(ctx); err != nil {
				return err
			}
		}
	})
}

func (r *Router) connectPeer(ctx context.Context, address NodeAddress) (c *Connection, err error) {
	tcpConn, err := r.Dial(ctx, address)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			if err := r.peerManager.DialFailed(ctx, address); err != nil {
				r.logger.Debug("failed to report dial failure", "peer", address, "err", err)
			}
		}
		return nil, fmt.Errorf("failed to dial peer %v: %w", address, err)
	}
	defer func() {
		if err != nil {
			tcpConn.Close()
		}
	}()

	conn, err := r.handshakePeer(ctx, tcpConn, utils.Some(address.NodeID))
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			if err := r.peerManager.DialFailed(ctx, address); err != nil {
				r.logger.Error("failed to report dial failure %v: %w", address, err)
			}
		}
		return nil, fmt.Errorf("failed to handshake with peer %v: %w", address, err)
	}

	// TODO(gprusak): this symmetric logic for handling duplicate connections is a source of race conditions:
	// if 2 nodes try to establish a connection to each other at the same time, both connections will be dropped.
	// Instead either:
	// * break the symmetry by favoring incoming connection iff my.NodeID > peer.NodeID
	// * keep incoming and outcoming connection pools separate to avoid the collision (recommended)
	if err := r.peerManager.Dialed(address); err != nil {
		return nil, fmt.Errorf("failed to dial outgoing/dialing peer %v: %w", address.NodeID, err)
	}
	return conn,nil
}

// dialPeer connects to a peer by dialing it.
func (r *Router) Dial(ctx context.Context, address NodeAddress) (*net.TCPConn, error) {
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
		r.peerManager.AddPrivatePeer(address.NodeID)
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
	if r.options.HandshakeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.options.HandshakeTimeout)
		defer cancel()
	}
	conn, err := HandshakeOrClose(ctx, r, tcpConn)
	if err != nil {
		return nil, err
	}
	peerInfo := conn.PeerInfo()
	nodeInfo := r.nodeInfoProducer()
	if peerInfo.Network != nodeInfo.Network {
		if err := r.peerManager.Delete(peerInfo.NodeID); err != nil {
			return nil, fmt.Errorf("problem removing peer from store from incorrect network [%s]: %w", peerInfo.Network, err)
		}
		return nil, fmt.Errorf("connected to peer from wrong network, %q, removed from peer store", peerInfo.Network)
	}
	if want,ok := expectID.Get(); ok && want != peerInfo.NodeID {
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

// evictPeers evicts connected peers as requested by the peer manager.
func (r *Router) evictPeers(ctx context.Context) error {
	for {
		ev, err := r.peerManager.EvictNext(ctx)
		if err != nil {
			return fmt.Errorf("failed to find next peer to evict: %w", err)
		}
		for conns := range r.conns.Lock() {
			if c, ok := conns[ev.ID]; ok {
				r.logger.Info("evicting peer", "peer", ev.ID, "cause", ev.Cause)
				c.Close()
			}
		}
	}
}

func (r *Router) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnNamed("acceptPeers", func() error { return r.acceptPeers(ctx) })
		s.SpawnNamed("dialPeers", func() error { return r.dialPeers(ctx) })
		s.SpawnNamed("evictPeers", func() error { return r.evictPeers(ctx) })
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

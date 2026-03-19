package p2p

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/service"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	dbm "github.com/tendermint/tm-db"
	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
)

// the maximum amount of addresses that can be included in a PEX batch.
const MaxPexAddrs = 100

type errBadNetwork struct{ error }

type PeerManager = peerManager[*ConnV2]
type PeerUpdatesRecv = peerUpdatesRecv[*ConnV2]
type ConnSet = connSet[*ConnV2]

// Router manages peer connections and routes messages between peers and channels.
type Router struct {
	*service.BaseService

	metrics *Metrics
	lc      *metricsLabelCache

	options     *RouterOptions
	privKey     NodeSecretKey
	peerManager *PeerManager

	peerDB           utils.Watch[*peerDB]
	nodeInfoProducer func() *types.NodeInfo

	channels utils.RWMutex[map[ChannelID]*channel]
	giga     utils.Option[*GigaRouter]

	started chan struct{}
}

func (r *Router) getChannelDescs() []*conn.ChannelDescriptor {
	for channels := range r.channels.RLock() {
		descs := make([]*conn.ChannelDescriptor, 0, len(channels))
		for _, ch := range channels {
			descs = append(descs, &ch.desc)
		}
		return descs
	}
	panic("unreachable")
}

// NewRouter creates a new Router.
func NewRouter(
	metrics *Metrics,
	privKey NodeSecretKey,
	nodeInfoProducer func() *types.NodeInfo,
	db dbm.DB,
	options *RouterOptions,
) (*Router, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	// 100 is arbitrary - we need some bound, otherwise peerDB will
	// maintain the whole connection history without pruning.
	// 100 is more or less an upper bound on how many concurrent
	// connections sei-v2 can effectively handle currently.
	peerDB, err := newPeerDB(db, min(options.maxOutbound(), 100))
	if err != nil {
		return nil, fmt.Errorf("newPeerDB(): %w", err)
	}
	var initialAddrs []NodeAddress
	for addr := range peerDB.All() {
		if err := addr.Validate(); err != nil {
			logger.Error("peerDB: bad address", "addr", addr.String(), "err", err)
		}
		initialAddrs = append(initialAddrs, addr)
	}
	selfID := privKey.Public().NodeID()
	peerManager := newPeerManager[*ConnV2](selfID, options)
	// initialAddrs will stay around util pex table fills the whole "extra" cache.
	if err := peerManager.PushPex(utils.None[types.NodeID](), initialAddrs); err != nil {
		return nil, fmt.Errorf("peerManager.PushPex(initialAddrs): %w", err)
	}
	router := &Router{
		metrics:          metrics,
		lc:               newMetricsLabelCache(),
		privKey:          privKey,
		nodeInfoProducer: nodeInfoProducer,
		peerManager:      peerManager,
		options:          options,
		channels:         utils.NewRWMutex(map[ChannelID]*channel{}),
		peerDB:           utils.NewWatch(peerDB),
		started:          make(chan struct{}),
	}
	if gigaCfg, ok := options.Giga.Get(); ok {
		router.giga = utils.Some(NewGigaRouter(gigaCfg, privKey))
	}
	router.BaseService = service.NewBaseService("router", router)
	return router, nil
}

func (r *Router) Endpoint() Endpoint {
	return r.options.Endpoint
}

func (r *Router) WaitForStart(ctx context.Context) error {
	_, _, err := utils.RecvOrClosed(ctx, r.started)
	return err
}

func (r *Router) AddAddrs(sender types.NodeID, addrs []NodeAddress) error {
	return r.peerManager.PushPex(utils.Some(sender), addrs)
}

func (r *Router) Subscribe() *PeerUpdatesRecv {
	return r.peerManager.Subscribe()
}

func (r *Router) Connected(id types.NodeID) bool {
	_, ok := GetAny(r.peerManager.Conns(), id)
	return ok
}

func (r *Router) Advertise(maxAddrs int) []NodeAddress {
	addrs := r.peerManager.Advertise()
	return addrs[:min(len(addrs), maxAddrs)]
}

// OpenChannel opens a new channel for the given message type.
func OpenChannel[T gogoproto.Message](r *Router, chDesc ChannelDescriptor[T]) (*Channel[T], error) {
	for channels := range r.channels.Lock() {
		id := chDesc.ID
		if _, ok := channels[id]; ok {
			return nil, fmt.Errorf("channel %v already exists", id)
		}
		channels[id] = newChannel(chDesc.ToGeneric())
		// add the channel to the nodeInfo if it's not already there.
		r.nodeInfoProducer().AddChannel(uint16(chDesc.ID))
		return &Channel[T]{
			router:  r,
			channel: channels[id],
		}, nil
	}
	panic("unreachable")
}

func (r *Router) acceptPeersRoutine(ctx context.Context) error {
	if err := r.Endpoint().Validate(); err != nil {
		return err
	}
	listener, err := tcp.Listen(r.Endpoint().AddrPort)
	if err != nil {
		return fmt.Errorf("net.Listen(): %w", err)
	}
	close(r.started) // signal that we are listening

	connTracker := newConnTracker(
		r.options.maxIncomingConnectionAttempts(),
		r.options.incomingConnectionWindow(),
	)
	sem := semaphore.NewWeighted(int64(r.options.maxAccepts()))
	limiter := rate.NewLimiter(r.options.maxAcceptRate(), r.options.maxAccepts())
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for {
			if err := sem.Acquire(ctx, 1); err != nil {
				return err
			}
			if err := limiter.Wait(ctx); err != nil {
				return err
			}
			tcpConn, err := listener.AcceptOrClose(ctx)
			if err != nil {
				return err
			}
			r.metrics.NewConnections.With("direction", "in", "success", "true").Add(1)
			addr := tcpConn.RemoteAddr()
			// Spawn a goroutine per connection.
			s.Spawn(func() error {
				defer tcpConn.Close()
				release := sync.OnceFunc(func() { sem.Release(1) })
				defer release()
				err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
					if err := r.options.filterPeerByIP(ctx, addr); err != nil {
						return fmt.Errorf("peer filtered by IP: %w", err)
					}
					if err := connTracker.AddConn(addr); err != nil {
						return fmt.Errorf("rate limiting incoming: %w", err)
					}
					defer connTracker.RemoveConn(addr)

					s.SpawnBg(func() error { return tcpConn.Run(ctx) })

					handshakeCtx := ctx
					if d, ok := r.options.HandshakeTimeout.Get(); ok {
						var cancel context.CancelFunc
						handshakeCtx, cancel = context.WithTimeout(ctx, d)
						defer cancel()
					}
					var pexAddrs []NodeAddress
					if r.options.PexOnHandshake {
						pexAddrs = r.Advertise(MaxPexAddrs)
					}
					hConn, err := handshake(handshakeCtx, tcpConn, r.privKey, handshakeSpec{
						SelfAddr: r.options.SelfAddress,
						// Listener has to send pex data, so that dialer can learn about more peers in
						// case listener does not have capacity for new connections.
						// Dialer also could potentially send pex data, but there is no benefit from doing so:
						// - if listener is full, then it won't use the new data and it won't gossip it further either, since only verified data is gossiped.
						// - if it is not full, then the connection will be established and pex data will be sent the regular way using PEX protocol.
						PexAddrs:          pexAddrs,
						SeiGigaConnection: r.giga.IsPresent(),
					})
					if err != nil {
						return fmt.Errorf("handshake(): %w", err)
					}
					if giga, ok := r.giga.Get(); ok && hConn.msg.SeiGigaConnection {
						release()
						return giga.RunInboundConn(ctx, hConn)
					}
					peerID := hConn.msg.NodeAuth.Key().NodeID()
					if err := r.options.filterPeerByID(ctx, peerID); err != nil {
						return fmt.Errorf("peer filtered by ID (%v): %w", peerID, err)
					}
					info, err := exchangeNodeInfo(ctx, hConn, *r.nodeInfoProducer())
					if err != nil {
						return fmt.Errorf("exchangeNodeInfo(): %w", err)
					}
					release()
					return r.runConn(ctx, hConn, info, utils.None[NodeAddress]())
				})
				logger.Error("r.runConn(inbound)", "addr", addr, "err", err)
				return nil
			})
		}
	})
}

func (r *Router) dialPeersRoutine(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// Task feeding the upgrade permit to peer manager.
		s.Spawn(func() error {
			const upgradeInterval = time.Minute
			for {
				r.peerManager.PushUpgradePermit()
				if err := utils.Sleep(ctx, upgradeInterval); err != nil {
					return err
				}
			}
		})
		const dialBurst = 10
		limiter := rate.NewLimiter(r.options.maxDialRate(), dialBurst)
		for {
			if err := limiter.Wait(ctx); err != nil {
				return err
			}
			addrs, err := r.peerManager.StartDial(ctx)
			if err != nil {
				return err
			}
			id := addrs[0].NodeID
			s.Spawn(func() error {
				err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
					tcpConn, err := r.dial(ctx, addrs)
					if err != nil {
						r.peerManager.DialFailed(id)
						return fmt.Errorf("r.dial(): %w", err)
					}
					s.SpawnBg(func() error { return tcpConn.Run(ctx) })
					var hConn *handshakedConn
					var info types.NodeInfo
					err = utils.WithOptTimeout(ctx, r.options.HandshakeTimeout, func(ctx context.Context) error {
						var err error
						hConn, err = handshake(ctx, tcpConn, r.privKey, handshakeSpec{
							SelfAddr:          r.options.SelfAddress,
							SeiGigaConnection: false,
						})
						if err != nil {
							return fmt.Errorf("handshake(): %w", err)
						}
						if got := hConn.msg.NodeAuth.Key().NodeID(); got != id {
							return fmt.Errorf("peer NodeID = %v, want %v", got, id)
						}
						if r.options.PexOnHandshake {
							// Since the connection is not established yet, the handshake pex data
							// will end up in a bounded cache, rather than main index. That's fine because
							// we use the handshake pex data only for a local search,
							// which is not supposed to be exhaustive.
							if err := r.AddAddrs(id, hConn.msg.PexAddrs); err != nil {
								return fmt.Errorf("r.AddAddrs(): %w", err)
							}
						}
						info, err = exchangeNodeInfo(ctx, hConn, *r.nodeInfoProducer())
						if err != nil {
							return fmt.Errorf("exchangeNodeInfo(): %w", err)
						}
						return nil
					})
					if err != nil {
						r.peerManager.DialFailed(id)
						return err
					}
					dialAddrRaw := hConn.conn.RemoteAddr()
					dialAddr := NodeAddress{NodeID: id, Hostname: dialAddrRaw.Addr().String(), Port: dialAddrRaw.Port()}
					if err := r.runConn(ctx, hConn, info, utils.Some(dialAddr)); err != nil {
						return fmt.Errorf("r.runConn(): %w", err)
					}
					return nil
				})
				logger.Error("r.runConn(outbound)", "id", id, "err", err)
				return nil
			})
		}
	})
}

// storePeersRoutine periodically snapshots the current connection set to disk,
// so that peers are immediately rediscovered on restart.
func (r *Router) storePeersRoutine(ctx context.Context) error {
	storeInterval := r.options.peerStoreInterval()
	for {
		for db, ctrl := range r.peerDB.Lock() {
			// Mark connections as still available.
			now := time.Now()
			conns := r.peerManager.Conns()
			if conns.Len() > 0 {
				ctrl.Updated()
			}
			for _, conn := range conns.All() {
				if addr, ok := conn.dialAddr.Get(); ok {
					if err := db.Insert(addr, now); err != nil {
						return fmt.Errorf("db.Insert(): %w", err)
					}
				}
			}
		}
		if err := utils.Sleep(ctx, storeInterval); err != nil {
			return err
		}
	}
}

func (r *Router) metricsRoutine(ctx context.Context) error {
	for {
		if err := utils.Sleep(ctx, 10*time.Second); err != nil {
			return err
		}
		r.metrics.Peers.Set(float64(r.peerManager.Conns().Len()))
		r.peerManager.LogState()
	}
}

// Evict reports a peer misbehavior and forces peer to be disconnected.
func (r *Router) Evict(id types.NodeID, err error) {
	logger.Error("evicting", "peer", id, "err", err)
	r.peerManager.Evict(id)
}

func (r *Router) IsBlockSyncPeer(id types.NodeID) bool {
	return r.peerManager.IsBlockSyncPeer(id)
}

// dial connects to a peer by dialing it.
func (r *Router) dial(ctx context.Context, addrs []NodeAddress) (_ tcp.Conn, err error) {
	defer func() {
		success := "true"
		if err != nil {
			success = "false"
		}
		r.metrics.NewConnections.With("direction", "out", "success", success).Add(1)
	}()
	resolveCtx := ctx
	if d, ok := r.options.ResolveTimeout.Get(); ok {
		var cancel context.CancelFunc
		resolveCtx, cancel = context.WithTimeout(resolveCtx, d)
		defer cancel()
	}

	endpointSet := map[Endpoint]struct{}{}
	// Resolve addresses in parallel. No errors expected,
	// just resolve as many addresses as possible within timeout.
	utils.OrPanic(scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		endpointSet := utils.NewMutex(endpointSet)
		for _, addr := range addrs {
			s.Spawn(func() error {
				endpoints, err := addr.Resolve(resolveCtx)
				if err != nil {
					logger.Info("address.Resolve() failed", "addr", addr, "err", err)
					return nil
				}
				if len(endpoints) > 0 {
					for endpointSet := range endpointSet.Lock() {
						endpointSet[endpoints[0]] = struct{}{}
					}
				}
				return nil
			})
		}
		return nil
	}))
	for endpoint := range endpointSet {
		c, err := utils.WithOptTimeout1(ctx, r.options.DialTimeout, func(ctx context.Context) (tcp.Conn, error) {
			return tcp.Dial(ctx, endpoint.AddrPort)
		})
		if err != nil {
			continue
		}
		return c, nil
	}
	return tcp.Conn{}, errors.New("all endpoints failed")
}

func (r *Router) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnNamed("acceptPeers", func() error { return r.acceptPeersRoutine(ctx) })
		s.SpawnNamed("dialPeers", func() error { return r.dialPeersRoutine(ctx) })
		s.SpawnNamed("storePeers", func() error { return r.storePeersRoutine(ctx) })
		s.SpawnNamed("metrics", func() error { return r.metricsRoutine(ctx) })
		if giga, ok := r.giga.Get(); ok {
			s.SpawnNamed("giga", func() error { return giga.Run(ctx) })
		}
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

package p2p

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/service"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	dbm "github.com/tendermint/tm-db"
	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
)

type errBadNetwork struct{ error }

type PeerManager = peerManager[*ConnV2]
type PeerUpdatesRecv = peerUpdatesRecv[*ConnV2]
type ConnSet = connSet[*ConnV2]

// Router manages peer connections and routes messages between peers and channels.
type Router struct {
	*service.BaseService
	logger log.Logger

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
	logger log.Logger,
	metrics *Metrics,
	privKey NodeSecretKey,
	nodeInfoProducer func() *types.NodeInfo,
	db dbm.DB,
	options *RouterOptions,
) (*Router, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	selfID := privKey.Public().NodeID()
	peerManager := newPeerManager[*ConnV2](selfID, options)
	peerDB, err := newPeerDB(db, options.maxPeers())
	if err != nil {
		return nil, fmt.Errorf("newPeerDB(): %w", err)
	}
	for addr := range peerDB.All() {
		if err := peerManager.AddAddrs(utils.Slice(addr)); err != nil {
			logger.Error("peerDB: bad address", "addr", addr.String(), "err", err)
		}
	}
	router := &Router{
		logger:           logger,
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
	router.BaseService = service.NewBaseService(logger, "router", router)
	return router, nil
}

// PeerRatio returns the ratio of peer addresses stored to the maximum size.
func (r *Router) PeerRatio() float64 {
	m, ok := r.options.MaxConnected.Get()
	if !ok || m == 0 {
		return 0
	}
	return float64(r.peerManager.Conns().Len()) / float64(m)
}

func (r *Router) Endpoint() Endpoint {
	return r.options.Endpoint
}

func (r *Router) WaitForStart(ctx context.Context) error {
	_, _, err := utils.RecvOrClosed(ctx, r.started)
	return err
}

func (r *Router) AddAddrs(addrs []NodeAddress) error {
	return r.peerManager.AddAddrs(addrs)
}

func (r *Router) Subscribe() *PeerUpdatesRecv {
	return r.peerManager.Subscribe()
}

func (r *Router) Connected(id types.NodeID) bool {
	_, ok := r.peerManager.Conns().Get(id)
	return ok
}

func (r *Router) State(id types.NodeID) string {
	return r.peerManager.State(id)
}

func (r *Router) Peers() []types.NodeID {
	return r.peerManager.Peers()
}

func (r *Router) Addresses(id types.NodeID) []NodeAddress {
	return r.peerManager.Addresses(id)
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
					hConn, err := handshake(handshakeCtx, tcpConn, r.privKey, r.options.SelfAddress, r.giga.IsPresent())
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
				r.logger.Error("r.runConn(inbound)", "addr", addr, "err", err)
				return nil
			})
		}
	})
}

func (r *Router) dialPeersRoutine(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		limiter := rate.NewLimiter(r.options.maxDialRate(), r.options.maxDials())
		// Separate routine for dialing persistent/regular peers.
		for _, persistentPeer := range utils.Slice(true, false) {
			s.Spawn(func() error {
				for {
					if err := limiter.Wait(ctx); err != nil {
						return err
					}
					addr, err := r.peerManager.StartDial(ctx, persistentPeer)
					if err != nil {
						return err
					}
					s.Spawn(func() error {
						err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
							tcpConn, err := r.dial(ctx, addr)
							if err != nil {
								r.peerManager.DialFailed(addr)
								return fmt.Errorf("r.dial(): %w", err)
							}
							s.SpawnBg(func() error { return tcpConn.Run(ctx) })

							var hConn *handshakedConn
							var info types.NodeInfo
							err = utils.WithOptTimeout(ctx, r.options.HandshakeTimeout, func(ctx context.Context) error {
								var err error
								hConn, err = handshake(ctx, tcpConn, r.privKey, r.options.SelfAddress, false)
								if err != nil {
									return fmt.Errorf("handshake(): %w", err)
								}
								if got := hConn.msg.NodeAuth.Key().NodeID(); got != addr.NodeID {
									return fmt.Errorf("peer NodeID = %v, want %v", got, addr.NodeID)
								}
								info, err = exchangeNodeInfo(ctx, hConn, *r.nodeInfoProducer())
								if err != nil {
									return fmt.Errorf("exchangeNodeInfo(): %w", err)
								}
								return nil
							})
							if err != nil {
								r.peerManager.DialFailed(addr)
								return err
							}
							if err := r.runConn(ctx, hConn, info, utils.Some(addr)); err != nil {
								return fmt.Errorf("r.runConn(): %w", err)
							}
							return nil
						})
						r.logger.Error("r.runConn(outbound)", "addr", addr.String(), "err", err)
						return nil
					})
				}
			})
		}
		return nil
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
		r.peerManager.LogState(r.logger)
	}
}

// Evict reports a peer misbehavior and forces peer to be disconnected.
func (r *Router) Evict(id types.NodeID, err error) {
	r.logger.Error("evicting", "peer", id, "err", err)
	r.peerManager.Evict(id)
}

func (r *Router) IsBlockSyncPeer(id types.NodeID) bool {
	return r.peerManager.IsBlockSyncPeer(id)
}

// dialPeer connects to a peer by dialing it.
func (r *Router) dial(ctx context.Context, addr NodeAddress) (_ tcp.Conn, err error) {
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

	r.logger.Debug("dialing peer address", "peer", addr)
	endpoints, err := addr.Resolve(resolveCtx)
	if err != nil {
		return tcp.Conn{}, fmt.Errorf("address.Resolve(): %w", err)
	}
	if len(endpoints) == 0 {
		return tcp.Conn{}, fmt.Errorf("address %q did not resolve to any endpoints", addr)
	}

	for _, endpoint := range endpoints {
		dialCtx := ctx
		if d, ok := r.options.DialTimeout.Get(); ok {
			var cancel context.CancelFunc
			dialCtx, cancel = context.WithTimeout(dialCtx, d)
			defer cancel()
		}
		if err := endpoint.Validate(); err != nil {
			return tcp.Conn{}, err
		}
		c, err := tcp.Dial(dialCtx, endpoint.AddrPort)
		if err != nil {
			r.logger.Debug("failed to dial endpoint", "peer", addr.NodeID, "endpoint", endpoint, "err", err)
			continue
		}
		r.logger.Debug("dialed peer", "peer", addr.NodeID, "endpoint", endpoint)
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

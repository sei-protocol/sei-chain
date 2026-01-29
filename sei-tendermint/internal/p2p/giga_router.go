package p2p

import (
	"context"
	"fmt"
	"github.com/tendermint/tendermint/internal/p2p/giga"
	"github.com/tendermint/tendermint/internal/p2p/rpc"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"log"
	"time"
)

type GigaRouterConfig struct {
	InboundPeers  map[NodePublicKey]bool
	OutboundPeers map[NodePublicKey]tcp.HostPort
}

type GigaRouter struct {
	cfg     *GigaRouterConfig
	key     NodeSecretKey
	poolIn  *giga.Pool[NodePublicKey, rpc.Server[giga.API]]
	poolOut *giga.Pool[NodePublicKey, rpc.Client[giga.API]]
}

func NewGigaRouter(cfg *GigaRouterConfig, key NodeSecretKey) *GigaRouter {
	return &GigaRouter{
		cfg:     cfg,
		key:     key,
		poolIn:  giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
		poolOut: giga.NewPool[NodePublicKey, rpc.Client[giga.API]](),
	}
}

func (r *GigaRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for key, hostport := range r.cfg.OutboundPeers {
			s.Spawn(func() error {
				for {
					err := r.dialAndRunConn(ctx, key, hostport)
					log.Printf("[%v:%v] %v", key, hostport, err)
					if err := utils.Sleep(ctx, 10*time.Second); err != nil {
						return err
					}
				}
			})
		}
		return nil
	})
}

func (r *GigaRouter) RunClients(ctx context.Context, task func(context.Context, rpc.Client[giga.API]) error) error {
	return r.poolOut.RunForEach(ctx, task)
}

func (r *GigaRouter) RunServers(ctx context.Context, task func(context.Context, rpc.Server[giga.API]) error) error {
	return r.poolIn.RunForEach(ctx, task)
}

func (r *GigaRouter) dialAndRunConn(ctx context.Context, key NodePublicKey, hp tcp.HostPort) error {
	addrs, err := hp.Resolve(ctx)
	if err != nil {
		return fmt.Errorf("%v.Resolve(): %w", hp, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%v.Resolve() = []", hp)
	}
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		tcpConn, err := tcp.Dial(ctx, addrs[0])
		if err != nil {
			return fmt.Errorf("tcp.Dial(%v): %w", addrs[0], err)
		}
		s.SpawnBg(func() error { return tcpConn.Run(ctx) })
		// TODO: handshake needs a timeout.
		hConn, err := handshake(ctx, tcpConn, r.key, true)
		if err != nil {
			return fmt.Errorf("handshake(): %w", err)
		}
		if !hConn.msg.SeiGigaConnection {
			return fmt.Errorf("not a sei giga connection")
		}
		if got := hConn.msg.NodeAuth.Key(); got != key {
			return fmt.Errorf("peer key = %v, want %v", got, key)
		}
		client := rpc.NewClient[giga.API]()
		return r.poolOut.InsertAndRun(ctx, key, client, func(ctx context.Context) error { 
			return scope.Run(ctx, func(ctx context.Context,
			return client.Run(ctx, hConn.conn)
		})
	})
}

func (r *GigaRouter) RunInboundConn(ctx context.Context, hConn *handshakedConn) error {
	if !hConn.msg.SeiGigaConnection {
		return fmt.Errorf("not a SeiGiga connection")
	}
	// Filter unwanded connections.
	key := hConn.msg.NodeAuth.Key()
	if !r.cfg.InboundPeers[key] {
		return fmt.Errorf("peer not whitelisted")
	}
	server := rpc.NewServer[giga.API]()
	return r.poolIn.InsertAndRun(ctx, key, server, func(ctx context.Context) error { return server.Run(ctx, hConn.conn) })
}

func RunServer(ctx context.Context, state *State, router *p2p.GigaRouter) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return runBlockFetcher(ctx,state,getBlockReqs) })
		s.Spawn(func() error {
			return router.RunServers(ctx, func(ctx context.Context, server rpc.Server[giga.API]) error {
				return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
					s.Spawn(func() error { return giga.StreamFullCommitQCs.Serve(ctx, server, state.streamFullCommitQCs) })
					s.Spawn(func() error { return giga.GetBlock.Serve(ctx, server, state.getBlock) })
					return nil
				})
			})
		})
		s.Spawn(func() error {
			return router.RunClients(ctx, func(ctx context.Context, client rpc.Client[giga.API]) error {
				return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
					s.Spawn(func() error { return state.runStreamFullCommitQCs(ctx,client) })
					s.Spawn(func() error { return state.runGetBlock(ctx,client,getBlockReqs) })
					return nil
				})
			})
		})
		return nil
	})
}

package p2p

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
)

type GigaRouterConfig struct {
	InboundPeers  map[NodePublicKey]bool
	OutboundPeers map[NodePublicKey]tcp.HostPort
	State         *consensus.State
}

type GigaRouter struct {
	cfg     *GigaRouterConfig
	key     NodeSecretKey
	service *giga.Service
	poolIn  *giga.Pool[NodePublicKey, rpc.Server[giga.API]]
	poolOut *giga.Pool[NodePublicKey, rpc.Client[giga.API]]
}

func NewGigaRouter(cfg *GigaRouterConfig, key NodeSecretKey) *GigaRouter {
	return &GigaRouter{
		cfg:     cfg,
		key:     key,
		service: giga.NewService(cfg.State),
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
		hConn, err := handshake(ctx, tcpConn, r.key, handshakeSpec{SeiGigaConnection: true})
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
			return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
				s.Spawn(func() error { return client.Run(ctx, hConn.conn) })
				return r.service.RunClient(ctx, client)
			})
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
	return r.poolIn.InsertAndRun(ctx, key, server, func(ctx context.Context) error {
		return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			s.Spawn(func() error { return server.Run(ctx, hConn.conn) })
			return r.service.RunServer(ctx, server)
		})
	})
}

package p2p

import (
	"context"
	"fmt"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"github.com/tendermint/tendermint/libs/utils"
)

type GigaServer interface {
	LaneVotes(ctx context.Context, Handle[Req,Vote], Handle[Vote,Req]) error
	CommitQCs()
	ExchangeAppVotes()
	ExchangeAppQCs()
	// Which lane?
	StreamLaneProposals(ctx context.Context, stream BidiStream[Proposal]
	FetchBlock(Handle[Req,Resp])
	FetchGlobalBlock()

}

type GigaRouterConfig struct {
	InboundPeers map[NodePublicKey]bool
	OutboundPeers map[NodePublicKey]tcp.HostPort
}

type GigaConnPool struct {
	inbound map[NodePublicKey]*ConnGiga
	outbound map[NodePublicKey]*ConnGiga
}

type GigaRouter struct {
	cfg *GigaRouterConfig
	pool utils.Mutex[*GigaConnPool]
}

func NewGigaRouter(cfg *GigaRouterConfig) *GigaRouter {
	return &GigaRouter{
		cfg: cfg,
		pool: utils.NewMutex(&GigaConnPool{
			inbound: map[NodePublicKey]*ConnGiga{},
			outbound: map[NodePublicKey]*ConnGiga{},
		}),
	}
}

func (r *GigaRouter) RunConn(ctx context.Context, conn *ConnGiga) error {
	for pool := range r.pool.Lock() {
		if conn.outbound {
			if _,ok := r.cfg.OutboundPeers[conn.key]; !ok { return fmt.Errorf("peer not whitelisted") }
			// Drop duplicate.
			if old,ok := pool.outbound[conn.key]; ok { old.conn.Close() }
			pool.outbound[conn.key] = conn
			defer func() {
				for pool := range r.pool.Lock() {
					if pool.outbound[conn.key]==conn { delete(pool.outbound,conn.key) }
				}
			}()
		} else {
			if !r.cfg.InboundPeers[conn.key] { return fmt.Errorf("peer not whitelisted") }
			// Drop duplicate.
			if old,ok := pool.inbound[conn.key]; ok { old.conn.Close() }
			pool.inbound[conn.key] = conn
			defer func() {
				for pool := range r.pool.Lock() {
					if pool.inbound[conn.key]==conn { delete(pool.inbound,conn.key) }
				}
			}()
		}
	}
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		return conn.Run(ctx)
	})
}

func (r *GigaRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		return nil
	})
}

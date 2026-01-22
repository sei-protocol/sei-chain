package p2p

import (
	"context"
	"fmt"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"github.com/tendermint/tendermint/libs/utils"
)

type GigaConn struct {
	closed chan struct{}
	mux *mux.Mux
}

func newGigaConn() *GigaConn {
	
}


func (c *GigaConn) run(ctx context.Context, conn conn.Conn) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return c.mux.Run(ctx, conn) })
		_,_,_ = utils.RecvOrClosed(ctx,c.closed)
		return 
	})
}
type GRouter struct {}

func (GRouter) ForConn(ctx,func(ctx,GConn) error) error {
	// internally uses a queue of connections with removable elements
	// ctx is limited to connection lifetime
}

func (GConn) Kind1() rpc.Kind[Req1,Resp1]
func (GConn) Kind2() rpc.Kind[Req2,Resp2]

type rpc.Kind[Req,Resp any] struct {
	*mux.Mux
	kind mux.StreamKind
}

func (Kind[Req,Resp]) Connect(ctx) Stream[Req,Resp]
func (Kind[Req,Resp]) Accept(ctx) Stream[Resp,Req]

func (Stream[Req,Resp]) Send(ctx,Req) error
func (Stream[Req,Resp]) Recv(ctx) (Resp,error)
func (Stream[Req,Resp]) Close()

*KindSpec {
	mux.StreamKind
	inflight int
	maxReqSize
	maxRespSize
}

MuxView[Req,Resp] {
	*mux.Mux
	*KindSpec
}

type ServerSpec struct {}

func (ServerSpec) Register(KindSpec{
	acceptMax
	connectMax
	reqMaxSize
	respMaxSize
	func( endpoint[Req,Resp]
})

Server.ForEach(ctx, func(ctx context.Context, conn *ConnGiga) error {
	conn.LaneVotes().Call()

})

type Server struct {
	
}

func (Server) RegisterStream(mux.StreamKind, Handle[Req,Vote], Handle[Vote,Req])
func (Server) RegisterRPC(mux.StreamKind, rate

type GigaServer interface {
	LaneVotes(ctx context.Context, ) error
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
			inbound: map[NodePublicKey]*GigaConn{},
			outbound: map[NodePublicKey]*GigaConn{},
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
	mux := mux.NewMux(&mux.Config{
		FrameSize: 10 * 1024,
		Kinds:     map[mux.StreamKind]*mux.StreamKindConfig{},
	})
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			
	mux.NewMux(Cfg{kind1 spec, kind2 spec})
}


		return conn.Run(ctx)
	})
}

func (r *GigaRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		return nil
	})
}

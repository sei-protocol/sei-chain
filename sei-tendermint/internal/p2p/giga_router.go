package p2p

import (
	"context"
	"fmt"
	"log"
	"time"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/internal/mux"
	"github.com/tendermint/tendermint/internal/autobahn/pb"
	"github.com/google/btree"
)

const kB InBytes = 1024
const MB InBytes = 1024 * kB

var reg = rpcRegistry{}

var rpcPing = mustAdd(reg,0,1,
	rpcMsg[*pb.PingReq]{kB,1},
	rpcMsg[*pb.PingResp]{kB,1},
)
var rpcStreamLaneProposals = mustAdd(reg,1,1,
	rpcMsg[*pb.StreamLaneProposalsReq]{kB,1},
	rpcMsg[*pb.LaneProposal]{2*MB,5},
)
var rpcStreamLaneVotes = mustAdd(reg,2,1,
	rpcMsg[*pb.StreamLaneVotesReq]{kB,1},
	rpcMsg[*pb.LaneVote]{10*kB,100},
)
var rpcStreamCommitQCs = mustAdd(reg,3,1,
	rpcMsg[*pb.StreamCommitQCsReq]{kB,1},
	rpcMsg[*pb.CommitQC]{10*kB,20},
)
var rpcStreamAppVotes = mustAdd(reg,4,1,
	rpcMsg[*pb.StreamAppVotesReq]{kB,1},
	rpcMsg[*pb.AppVote]{10*kB,100},
)
var rpcStreamAppQCs = mustAdd(reg,5,1,
	rpcMsg[*pb.StreamAppQCsReq]{kB,1},
	rpcMsg[*pb.AppQC]{10*kB,20},
)
var rpcConsensus = mustAdd(reg,6,1,
	rpcMsg[*pb.ConsensusReq]{kB,1},
	rpcMsg[*pb.ConsensusResp]{10*kB,100},
)
var rpcStreamFullCommitQCs = mustAdd(reg,7,1,
	rpcMsg[*pb.StreamFullCommitQCsReq]{kB,1},
	rpcMsg[*pb.FullCommitQC]{100*kB,20},
)
var rpcGetBlock = mustAdd(reg,8,1,
	rpcMsg[*pb.GetBlockReq]{10*kB,1},
	rpcMsg[*pb.Block]{2*MB,1},
)

type GigaConn struct {
	idx uint64
	closed utils.AtomicSend[bool]
	mux *mux.Mux
}

func newGigaConn(idx uint64) *GigaConn {	
	return &GigaConn {
		idx: idx,
		closed: utils.NewAtomicSend(false),
		mux: mux.NewMux(&mux.Config{FrameSize: 10 * 1024, Kinds: reg}),
	}
}

// TODO: split server/client
func (c *GigaConn) Close() { c.closed.Store(true) }
func (c *GigaConn) Ping() RPC[*pb.PingReq,*pb.PingResp] { return rpcPing.With(c.mux) }
func (c *GigaConn) StreamLaneProposals() RPC[*pb.StreamLaneProposalsReq,*pb.LaneProposal] { return rpcStreamLaneProposals.With(c.mux) }
func (c *GigaConn) StreamLaneVotes() RPC[*pb.StreamLaneVotesReq,*pb.LaneVote] { return rpcStreamLaneVotes.With(c.mux) }
func (c *GigaConn) StreamCommitQCs() RPC[*pb.StreamCommitQCsReq,*pb.CommitQC] { return rpcStreamCommitQCs.With(c.mux) }
func (c *GigaConn) StreamAppVotes() RPC[*pb.StreamAppVotesReq,*pb.AppVote] { return rpcStreamAppVotes.With(c.mux) }
func (c *GigaConn) StreamAppQCs() RPC[*pb.StreamAppQCsReq,*pb.AppQC] { return rpcStreamAppQCs.With(c.mux) }
func (c *GigaConn) Consensus() RPC[*pb.ConsensusReq,*pb.ConsensusResp] { return rpcConsensus.With(c.mux) }
func (c *GigaConn) StreamFullCommitQCs() RPC[*pb.StreamFullCommitQCsReq,*pb.FullCommitQC] { return rpcStreamFullCommitQCs.With(c.mux) }
func (c *GigaConn) GetBlock() RPC[*pb.GetBlockReq,*pb.Block] { return rpcGetBlock.With(c.mux) }

func (r *GigaRouter) ForConn(ctx context.Context, task func(context.Context, *GigaConn) error) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		sub := r.sub()
		for {
			c,err := sub.Recv(ctx)
			if err!=nil { return err }
			s.Spawn(func() error {
				return utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
					s.SpawnBg(func() error { return task(ctx,c) })
					return c.closed.Wait(ctx,func(closed bool) bool { return closed })
				}))
			})
		}
	})
}

type GigaRouterConfig struct {
	InboundPeers map[NodePublicKey]bool
	OutboundPeers map[NodePublicKey]tcp.HostPort
}

type Dir bool
const DirIn Dir = false
const DirOut Dir = true

type ByDir[T any] [2]T
func (x ByDir[T]) Get(d Dir) T {
	if d { return x[1] }
	return x[0]
}

func NewByDir[T any](init func()T) (out ByDir[T]) {
	out[0] = init()
	out[1] = init()
	return
}

func NewMap[K comparable, V any]() map[K]V { return map[K]V{} }

type gigaConnPool struct {
	nextIdx uint64
	byIdx *btree.BTreeG[*GigaConn]
	byDir ByDir[map[NodePublicKey]*GigaConn]
}

type GigaRouter struct {
	cfg *GigaRouterConfig
	pool utils.Watch[*gigaConnPool]
}

func NewGigaRouter(cfg *GigaRouterConfig) *GigaRouter {
	return &GigaRouter{
		cfg: cfg,
		pool: utils.NewWatch(&gigaConnPool{
			byIdx: btree.NewG(2,func(a,b *GigaConn) bool { return a.idx < b.idx }),
			byDir: NewByDir(NewMap[NodePublicKey,*GigaConn]),
		}),
	}
}

func (r *GigaRouter) dialAndRunConn(ctx context.Context, key NodePublicKey, hp tcp.HostPort) error {
	addrs,err := hp.Resolve(ctx)
	if err!=nil { return fmt.Errorf("%v.Resolve(): %w",hp,err) }
	if len(addrs)==0 { return fmt.Errorf("%v.Resolve() = []",hp) }
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		tcpConn,err := tcp.Dial(ctx,addrs[0])
		if err!=nil { return fmt.Errorf("tcp.Dial(%v): %w",addrs[0],err) }
		s.SpawnBg(func() error { return tcpConn.Run(ctx) })
		// TODO: handshake
		// authenticate
		return r.RunConn(ctx, connV3)
	})
}

func (r *GigaRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for key,hostport := range r.cfg.OutboundPeers {
			s.Spawn(func() error {
				for {
					err := r.dialAndRunConn(ctx,key,hostport)
					log.Printf("[%v:%v] %v",key,hostport,err)
					if err:=utils.Sleep(ctx,10*time.Second); err!=nil {
						return err
					}
				}
			})
		}
		return nil
	})
}

func (r *GigaRouter) RunConn(ctx context.Context, conn *ConnV3) error {
	// Filter unwanded connections.
	if conn.dir==DirIn && !r.cfg.InboundPeers[conn.key] { return fmt.Errorf("peer not whitelisted") }
	// Register connection.
	var gigaConn *GigaConn
	for pool,ctrl := range r.pool.Lock() {
		idx := pool.nextIdx
		pool.nextIdx += 1
		gigaConn = newGigaConn(idx)
		dirPool := pool.byDir.Get(conn.dir)
		// Drop duplicate.
		if old,ok := dirPool[conn.key]; ok { old.Close() }
		pool.byIdx.ReplaceOrInsert(gigaConn)
		dirPool[conn.key] = gigaConn
		ctrl.Updated()
	}
	defer func() {
		gigaConn.Close()
		for pool := range r.pool.Lock() {
			dirPool := pool.byDir.Get(conn.dir)
			pool.byIdx.Delete(gigaConn)
			if dirPool[conn.key]==gigaConn { delete(dirPool,conn.key) }
		}
	}()
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// TODO: ping routine.
		s.SpawnBg(func() error { return gigaConn.mux.Run(ctx,conn.conn) })
		_,err := gigaConn.closed.Wait(ctx, func(closed bool) bool { return closed })
		return err
	})
}

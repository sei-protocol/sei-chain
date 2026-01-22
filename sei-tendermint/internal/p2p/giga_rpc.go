package p2p

import (
	"context"
	"fmt"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/internal/mux"
	"github.com/tendermint/tendermint/internal/protoutils"
)

type InBytes uint64
type InMsgs uint64

type rpcMsg[Msg protoutils.Message] struct {
	MsgSize InBytes
	Window InMsgs
}

type rpcConfig[Req,Resp protoutils.Message] struct {
	Kind mux.StreamKind
	Req rpcMsg[Req]
	Resp rpcMsg[Resp]
	Concurrent uint64
}

func (cfg *rpcConfig[Req,Resp]) With(mux *mux.Mux) RPC[Req,Resp] {
	return RPC[Req,Resp]{mux,cfg}
}

type rpcRegistry = map[mux.StreamKind]*mux.StreamKindConfig

func mustAdd[Req,Resp protoutils.Message](reg rpcRegistry, kind mux.StreamKind, concurrent uint64, req rpcMsg[Req], resp rpcMsg[Resp]) *rpcConfig[Req,Resp] {
	// Simplification: we allow the same number of streams in each direction.
	if _,ok := reg[kind]; ok { panic(fmt.Errorf("conflicting rpc for kind %v",kind)) }
	reg[kind] = &mux.StreamKindConfig{MaxConnects:concurrent,MaxAccepts:concurrent}
	return &rpcConfig[Req,Resp]{kind,req,resp,concurrent}
}

type RPC[Req,Resp protoutils.Message] struct {
	mux *mux.Mux	
	cfg *rpcConfig[Req,Resp]
}
func (r RPC[Req,Resp]) Connect(ctx context.Context) (Stream[Req,Resp],error) {
	s,err := r.mux.Connect(ctx,r.cfg.Kind,uint64(r.cfg.Resp.MsgSize),uint64(r.cfg.Resp.Window))
	if err!=nil { return Stream[Req,Resp]{},err }
	return Stream[Req,Resp]{inner:s},nil
}
func (r RPC[Req,Resp]) Accept(ctx context.Context) (Stream[Resp,Req],error) {
	s,err := r.mux.Accept(ctx,r.cfg.Kind,uint64(r.cfg.Req.MsgSize),uint64(r.cfg.Req.Window))
	if err!=nil { return Stream[Resp,Req]{},err }
	return Stream[Resp,Req]{inner:s},nil
}

type Stream[SendT,RecvT protoutils.Message] struct { inner *mux.Stream }

func (s Stream[SendT,RecvT]) Close() { s.inner.Close() }
func (s Stream[SendT,RecvT]) Send(ctx context.Context, msg SendT) error {
	return s.inner.Send(ctx,protoutils.Marshal(msg))
}
func (s Stream[SendT,RecvT]) Recv(ctx context.Context) (RecvT,error) {
	raw,err := s.inner.Recv(ctx,true)
	if err!=nil { return utils.Zero[RecvT](),err }
	return protoutils.Unmarshal[RecvT](raw)
}

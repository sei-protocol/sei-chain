package rpc

import (
	"context"
	"fmt"
	"reflect"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/mux"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"golang.org/x/time/rate"
)

type InBytes uint64
type InMsgs uint64

func (spec Msg[M]) Verify() error {
	var msg M
	if m := InBytes(msg.MaxSize()); m > spec.MsgSize { // nolint: gosec // MaxSize() >= 0
		return fmt.Errorf("%T.MaxSize() = %d exceeds configured cap %d", msg, m, spec.MsgSize)
	}
	return nil
}

type Msg[M protoutils.Sized] struct {
	MsgSize InBytes
	Window  InMsgs
}

type Limit struct {
	Rate       rate.Limit
	Concurrent uint64
}

type RPC[API any, Req, Resp protoutils.Sized] struct {
	Kind  mux.StreamKind
	Limit Limit
	Req   Msg[Req]
	Resp  Msg[Resp]
}

type rpcConfig struct {
	limit Limit
	name  string
}

type service map[mux.StreamKind]*rpcConfig

func (s service) muxServerConfig() *mux.Config {
	kinds := map[mux.StreamKind]*mux.StreamKindConfig{}
	for kind, rpc := range s {
		kinds[kind] = &mux.StreamKindConfig{
			Name:        rpc.name,
			MaxConnects: rpc.limit.Concurrent,
		}
	}
	return &mux.Config{
		FrameSize: 10 * 1024,
		Kinds:     kinds,
	}
}

var registry = map[reflect.Type]service{}

func (s service) muxClientConfig() *mux.Config {
	cfg := s.muxServerConfig()
	for _, x := range cfg.Kinds {
		x.MaxConnects, x.MaxAccepts = x.MaxAccepts, x.MaxConnects
	}
	return cfg
}

func Register[API any, Req, Resp protoutils.Sized](kind mux.StreamKind, name string, limit Limit, req Msg[Req], resp Msg[Resp]) *RPC[API, Req, Resp] {
	t := reflect.TypeFor[API]()
	if _, ok := registry[t]; !ok {
		registry[t] = service{}
	}
	service := registry[t]
	// Simplification: we allow the same number of streams in each direction.
	if _, ok := service[kind]; ok {
		panic(fmt.Errorf("conflicting rpc for kind %v", kind))
	}
	if err := req.Verify(); err != nil {
		panic(fmt.Errorf("RPC %v: %w", kind, err))
	}
	if err := resp.Verify(); err != nil {
		panic(fmt.Errorf("RPC %v: %w", kind, err))
	}
	service[kind] = &rpcConfig{
		limit: limit,
		name:  name,
	}
	return &RPC[API, Req, Resp]{
		Kind:  kind,
		Limit: limit,
		Req:   req,
		Resp:  resp,
	}
}

type Server[API any] struct{ mux *mux.Mux }

func NewServer[API any]() Server[API] {
	return Server[API]{mux.NewMux(registry[reflect.TypeFor[API]()].muxServerConfig())}
}

func (s Server[API]) Run(ctx context.Context, conn conn.Conn) error { return s.mux.Run(ctx, conn) }

type Client[API any] struct{ mux *mux.Mux }

func NewClient[API any]() Client[API] {
	return Client[API]{mux.NewMux(registry[reflect.TypeFor[API]()].muxClientConfig())}
}

func (c Client[API]) Run(ctx context.Context, conn conn.Conn) error { return c.mux.Run(ctx, conn) }

// TODO: add client-side rate limiting.
func (r *RPC[API, Req, Resp]) Call(ctx context.Context, client Client[API]) (Stream[Req, Resp], error) {
	s, err := client.mux.Accept(ctx, r.Kind, uint64(r.Resp.MsgSize), uint64(r.Resp.Window))
	if err != nil {
		return Stream[Req, Resp]{}, err
	}
	return Stream[Req, Resp]{inner: s}, nil
}

func (r *RPC[API, Req, Resp]) Serve(ctx context.Context, server Server[API], handler func(context.Context, Stream[Resp, Req]) error) error {
	limiter := rate.NewLimiter(r.Limit.Rate, int(r.Limit.Concurrent)) //nolint:gosec // Concurrent is a bounded config value
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for range r.Limit.Concurrent {
			s.Spawn(func() error {
				for ctx.Err() == nil {
					if err := limiter.Wait(ctx); err != nil {
						return err
					}
					muxStream, err := server.mux.Connect(ctx, r.Kind, uint64(r.Req.MsgSize), uint64(r.Req.Window)) //nolint:gosec // MsgSize and Window are validated config values
					if err != nil {
						return err
					}
					stream := Stream[Resp, Req]{inner: muxStream}
					err = handler(ctx, stream)
					stream.Close()
					if err != nil {
						return err
					}
				}
				return ctx.Err()
			})
		}
		return nil
	})
}

type Stream[SendT, RecvT protoutils.Message] struct{ inner *mux.Stream }

func (s Stream[SendT, RecvT]) Close() { s.inner.Close() }

func (s Stream[SendT, RecvT]) Send(ctx context.Context, msg SendT) error {
	return s.inner.Send(ctx, protoutils.Marshal(msg))
}

func (s Stream[SendT, RecvT]) Recv(ctx context.Context) (RecvT, error) {
	raw, err := s.inner.Recv(ctx, true)
	if err != nil {
		return utils.Zero[RecvT](), err
	}
	return protoutils.Unmarshal[RecvT](raw)
}

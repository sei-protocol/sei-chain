package mux 

import (
	"fmt"
	"net"
	"context"
	"errors"
	"testing"
	"sync/atomic"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/crypto/ed25519"
)

// Arbitrary nontrivial transformation to make sure that
// server actually does something.
func transform(msg []byte) []byte {
	out := make([]byte,len(msg))
	copy(out,msg)
	for i := range out {
		out[i] = out[i]*9+5
	}
	return out
}

func runServer(ctx context.Context, rng utils.Rng, mux *Mux, kind StreamKind) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		var count atomic.Int64
		for {
			// Accept a stream.
			maxMsgSize := uint64(rng.Intn(10000)+100)
			window := uint64(rng.Intn(10)+1)
			stream,err := mux.Accept(ctx,kind,maxMsgSize,window)
			if err!=nil {
				return utils.IgnoreCancel(err)
			}
			fmt.Printf("stream kind=%v accepted\n",kind)
			// Assert that concurrent stream limit is respected.
			if got,wantMax := uint64(count.Add(1)),mux.cfg.Kinds[kind].MaxAccepts; got>wantMax {
				return fmt.Errorf("got %v concurrent accepts, want <= %v",got,wantMax)
			}
			s.Spawn(func() error {
				defer stream.Close()
				defer count.Add(-1)
				// Handle the stream.
				for {
					msg,err := stream.Recv(ctx, true)
					if err!=nil {
						if errors.Is(err, errClosed) {
							return nil
						}
						return fmt.Errorf("stream.Recv(): %w",err)
					}
					if err:=stream.Send(ctx,transform(msg)); err!=nil {
						return fmt.Errorf("stream.Send(): %w",err)
					}
				}
			})
		}
	})
}

type clientSet struct {
	mux *Mux
	kind StreamKind
	count atomic.Int64
}

func (cs *clientSet) StreamingClient() *client {
	return &client{clientSet:cs,streaming:true}
}

func (cs *clientSet) SynchronousClient() *client {
	return &client{clientSet:cs,streaming:false}
}

func (cs *clientSet) BlockedClient() *client {
	return &client{clientSet:cs,streaming:true,blocked:true}
}

type client struct {
	*clientSet
	streaming bool
	blocked bool
}

func (c *client) Run(ctx context.Context, rng utils.Rng) error {
	// Connect to server.
	maxMsgSize := uint64(rng.Intn(10000)+100)
	window := uint64(rng.Intn(10)+1)
	stream,err := c.mux.Connect(ctx,c.kind,maxMsgSize,window)
	if err!=nil { return fmt.Errorf("mux.Connect(): %w",err) }
	// Assert that concurrent stream limit is respected.
	if got,wantMax := uint64(c.count.Add(1)),c.mux.cfg.Kinds[c.kind].MaxConnects; got>wantMax {
		return fmt.Errorf("got %v concurrent connects, want <= %v",got,wantMax)
	}
	defer stream.Close()
	defer c.count.Add(-1)

	// Prepare requests.
	maxReqSize := int(min(maxMsgSize,stream.maxSendMsgSize()))
	var reqs [][]byte
	for range rng.Intn(100) {
		size := rng.Intn(maxReqSize)
		reqs = append(reqs,utils.GenBytes(rng,size))
	}
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		if c.streaming {
			s.Spawn(func() error {
				// Stream the requests.
				for _,req := range reqs {
					if err := stream.Send(ctx,req); err!=nil {
						return fmt.Errorf("stream.Send(): %w",err)
					}
				}
				return nil
			})
			if c.blocked {
				<-ctx.Done()
				return nil
			}
			// Verify the responses.
			for _,req := range reqs {
				resp,err := stream.Recv(ctx, true)
				if err!=nil { return fmt.Errorf("stream.Recv(): %w",err) }
				if err:=utils.TestDiff(transform(req),resp); err!=nil {
					return err
				}
			}
		} else {
			for _,req := range reqs {
				if err := stream.Send(ctx,req); err!=nil {
					return fmt.Errorf("stream.Send(): %w",err)
				}
				resp,err := stream.Recv(ctx, true)
				if err!=nil { return fmt.Errorf("stream.Recv(): %w",err) }
				if err:=utils.TestDiff(transform(req),resp); err!=nil {
					return err
				}
			}
		}
		return nil
	})
}

func runConn(ctx context.Context, rng utils.Rng, kindCount int, c *net.TCPConn) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		sc,err := conn.MakeSecretConnection(ctx,c,ed25519.GenerateSecretKey())
		if err!=nil { return err }
		kinds := map[StreamKind]*StreamKindConfig {}
		for kind := range StreamKind(kindCount) {
			kinds[kind] = &StreamKindConfig {
				// > 1, so that blocked client doesn't hog all the streams
				MaxAccepts: uint64(rng.Intn(4)+2),
				MaxConnects: uint64(rng.Intn(4)+2),
			}
		}
		mux := NewMux(&Config {FrameSize: 10 * 1024, Kinds: kinds})
		s.SpawnBg(func() error { return mux.Run(ctx,sc) })
		for kind := range kinds {
			// Server.
			serverRng := rng.Split()
			s.SpawnBg(func() error { return runServer(ctx,serverRng,mux,kind) })
			cs := &clientSet{mux:mux,kind:kind}
			// Client which is blocked and doesn't receive responses.
			//clientRng := rng.Split()
			//s.SpawnBg(func() error { return cs.BlockedClient().Run(ctx,clientRng) })
			// Clients which send requests sequentially. 
			for range 5 {
				clientRng := rng.Split()	
				s.Spawn(func() error { return cs.SynchronousClient().Run(ctx,clientRng) })
			}
			// Clients which send requests concurrently.
			for range 20 {
				clientRng := rng.Split()	
				s.Spawn(func() error { return cs.StreamingClient().Run(ctx,clientRng) })
			}
		}
		return nil
	})
}

// Happy path test.
// * Uses SecretConnection for transport.
// * Tests both streaming and sequential stream communication.
// * Checks if concurrent streams limits are respected.
// * Checks that there is no head of line blocking.
func TestHappyPath(t *testing.T) {
	rng := utils.TestRng()
	kindCount := 1
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		c1,c2 := tcp.TestPipe()
		s.SpawnBg(func() error {
			<-ctx.Done()
			c1.Close()
			c2.Close()
			return nil
		})
		connRng := rng.Split()
		s.Spawn(func() error { return runConn(ctx,connRng,kindCount,c1) })
		connRng = rng.Split()
		s.Spawn(func() error { return runConn(ctx,connRng,kindCount,c2) })
		return nil
	})
	if err!=nil { t.Fatal(err) }
}

// reencoding protos
// different stream kinds
// optional: Send() should terminate if stream was closed from the other side.
// violations:
//   more streams than allowed
//   msg bigger than allowed
//   more messages than allowed
//   send after close (although good peer didn't close yet)
//   unknown kind

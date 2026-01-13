package mux 

import (
	"fmt"
	"context"
	"errors"
	"testing"
	"sync/atomic"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/crypto/ed25519"
)

// Ignores cancellation and connection errors.
// Wrap Mux.Run() calls in this - in tests we manage both ends of the connection,
// so there is a race condition between disconnects and cancellation, when test
// is shutting down.
func ignoreDisconnect(err error) error {
	if utils.ErrorAs[errConn](err).IsPresent() {
		return nil
	}
	return utils.IgnoreCancel(err)
}

func testConn(t *testing.T) (*conn.SecretConnection,*conn.SecretConnection) {
	c1,c2 := tcp.TestPipe()
	var scs [2]*conn.SecretConnection
	utils.OrPanic(scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		for i,c := range utils.Slice(c1,c2) {
			t.Cleanup(func(){ c.Close() })
			s.Spawn(func() error {
				scs[i] = utils.OrPanic1(conn.MakeSecretConnection(ctx,c,ed25519.GenerateSecretKey()))
				return nil
			})
		}
		return nil
	}))
	return scs[0],scs[1]
}

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

func runServer(ctx context.Context, rng utils.Rng, mux *Mux) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {	
		for kind := range mux.cfg.Kinds {
			_ = rng.Split()
			s.Spawn(func() error {
				var count atomic.Int64
				for {
					// Accept a stream.
					maxMsgSize := uint64(rng.Intn(10000)+100)
					window := uint64(rng.Intn(10)+1)
					stream,err := mux.Accept(ctx,kind,maxMsgSize,window)
					if err!=nil {
						return utils.IgnoreCancel(err)
					}
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
								if errors.Is(err, errRemoteClosed) || errors.Is(err,context.Canceled) {
									return nil
								}
								return fmt.Errorf("stream.Recv(): %w",err)
							}
							if err:=stream.Send(ctx,transform(msg)); err!=nil {
								if errors.Is(err,errRemoteClosed) {
									return nil
								}
								return fmt.Errorf("stream.Send(): %w",err)
							}
						}
					})
				}
			})
		}
		return nil
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
	for range rng.Intn(10) {
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

func makeMux(rng utils.Rng, kindCount int) *Mux {
	kinds := map[StreamKind]*StreamKindConfig {}
	for kind := range StreamKind(kindCount) {
		kinds[kind] = &StreamKindConfig {
			// > 1, so that blocked client doesn't hog all the streams
			MaxAccepts: uint64(rng.Intn(5)+2),
			MaxConnects: uint64(rng.Intn(5)+2),
		}
	}
	return NewMux(&Config {FrameSize: 10 * 1024, Kinds: kinds})
}

func runClients(ctx context.Context, rng utils.Rng, mux *Mux) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for kind := range mux.cfg.Kinds {
			cs := &clientSet{mux:mux,kind:kind}
			// Client which is blocked and doesn't receive responses.
			clientRng := rng.Split()
			s.SpawnBgNamed("blocked",func() error {
				return utils.IgnoreCancel(cs.BlockedClient().Run(ctx,clientRng))
			})
			// Clients which send requests sequentially. 
			for range 5 {
				clientRng := rng.Split()	
				s.SpawnNamed("sync",func() error { return cs.SynchronousClient().Run(ctx,clientRng) })
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
	kindCount := 5
	c1,c2 := testConn(t)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		for _,c := range utils.Slice(c1,c2) {
			mux := makeMux(rng,kindCount)
			serverRng := rng.Split()
			s.SpawnBgNamed("mux",func() error { return ignoreDisconnect(mux.Run(ctx,c)) })
			s.SpawnBgNamed("server",func() error { return utils.IgnoreCancel(runServer(ctx,serverRng,mux)) })
			clientRng := rng.Split()
			s.SpawnNamed("client",func() error { return runClients(ctx,clientRng,mux) })
		}
		return nil
	})
	if err!=nil { t.Fatal(err) }
}

func genStreamKind(rng utils.Rng) StreamKind {
	return StreamKind(rng.Uint64())
}

func genStreamKindConfig(rng utils.Rng) *StreamKindConfig {
	return &StreamKindConfig {
		MaxAccepts: rng.Uint64(),
		MaxConnects: rng.Uint64(),
	}
}

func genHandshake(rng utils.Rng) *handshake {
	return &handshake {
		Kinds: utils.GenMap(rng,genStreamKind,genStreamKindConfig),
	}
}

func TestConv(t *testing.T) {
	rng := utils.TestRng()
	require.NoError(t,handshakeConv.Test(genHandshake(rng)))
}

func makeConfig(kinds ...StreamKind) *Config {
	cfg := &Config {
		FrameSize: 1024,
		Kinds: map[StreamKind]*StreamKindConfig {},
	}
	for _,kind := range kinds {
		cfg.Kinds[kind] = &StreamKindConfig{MaxAccepts:1,MaxConnects:1}
	}
	return cfg
}

func TestStreamKindsMismatch(t *testing.T) {
	c1,c2 := testConn(t)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		var k0,k1,k2 StreamKind = 0,1,2
		muxs := utils.Slice(
			NewMux(makeConfig(k0,k1)),
			NewMux(makeConfig(k1,k2)),
		)
		for i,c := range utils.Slice(c1,c2) {
			s.SpawnBg(func() error { return ignoreDisconnect(muxs[i].Run(ctx,c)) })
		}
		// Connecting/accepting of unconfigured kind should error.
		if _,err := muxs[0].Connect(ctx,k2,10,10); !errors.Is(err,errUnknownKind) {
			return fmt.Errorf("got %v, want %v",err,errUnknownKind)
		}
		if _,err := muxs[1].Accept(ctx,k0,10,10); !errors.Is(err,errUnknownKind) {
			return fmt.Errorf("got %v, want %v",err,errUnknownKind)
		}

		// Connecting/accepting, when other end does not support given kind, should block.
		s.SpawnBg(func() error {
			if _,err := muxs[0].Connect(ctx,k0,10,10); !errors.Is(err,context.Canceled) {
				return fmt.Errorf("got %v, want canceled",err)
			}
			return nil
		})
		s.SpawnBg(func() error {
			if _,err := muxs[1].Accept(ctx,k2,10,10); !errors.Is(err,context.Canceled) {
				return fmt.Errorf("got %v, want canceled",err)
			}
			return nil
		})

		// Stream of the shared kind should work.
		msg := []byte("hello")
		s.Spawn(func() error {
			stream,err := muxs[0].Connect(ctx,k1,0,0)
			if err!=nil { return fmt.Errorf("mux1.Connect(): %w",err) }
			if err:=stream.Send(ctx,msg); err!=nil {
				return fmt.Errorf("stream.Send(): %w",err)
			}
			return nil
		})
		s.Spawn(func() error {
			stream,err := muxs[1].Accept(ctx,k1,uint64(len(msg)),1)
			if err!=nil { return fmt.Errorf("mux2.Accept(): %w",err) }
			got,err := stream.Recv(ctx,false)
			if err!=nil { return fmt.Errorf("stream.Recv(): %w",err) }
			return utils.TestDiff(msg,got)
		})
		return nil
	})
	if err!=nil { t.Fatal(err) }
}

// Test checking that closing a stream does not drop messages on the floor:
// sending and receiving still works as long as messages fit into a window.
func TestClosedStream(t *testing.T) {
	c1,c2 := testConn(t)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		kind := StreamKind(0)
		window := uint64(4)
		msg := []byte("hello")
		muxs := utils.Slice(
			NewMux(makeConfig(kind)),
			NewMux(makeConfig(kind)),
		)
		for i,c := range utils.Slice(c1,c2) {
			s.SpawnBg(func() error { return ignoreDisconnect(muxs[i].Run(ctx,c)) })
		}
		s.Spawn(func() error {
			// Just accept a single stream and close immediately.
			stream,err := muxs[0].Accept(ctx,kind,uint64(len(msg)),window)
			if err!=nil {
				return fmt.Errorf("mux1.Accept(): %w",err)
			}
			stream.Close()
			// Receive the messages anyway.
			// Window will not be updated (freeBuf flag is ignored).
			for range window {
				if _,err:=stream.Recv(ctx,true); err!=nil {
					return fmt.Errorf("stream.Recv(): %w",err)
				}
			}
			// Try to receive with empty window - should block until remote closes stream.
			if _,err:=stream.Recv(ctx,true); !errors.Is(err,errRemoteClosed) {
				return fmt.Errorf("stream.Recv(): %v, want %v",err,errRemoteClosed)
			}
			return nil
		})
		// Open a stream.
		stream,err := muxs[1].Connect(ctx,kind,0,0)
		if err!=nil { return fmt.Errorf("mux2.Connect(): %w",err) }
		defer stream.Close()
		// Fill the available window.
		for range window {
			if err:=stream.Send(ctx,msg); err!=nil {
				return fmt.Errorf("stream.Send(): %w",err)
			}
		}
		// Try to send after window is full.
		if err := stream.Send(ctx,msg); !errors.Is(err,errRemoteClosed) {
			return fmt.Errorf("stream.Send(): %v, want %v",err,errRemoteClosed)
		}
		// Try to send after local close.
		stream.Close()
		if err := stream.Send(ctx,msg); !errors.Is(err,errClosed) {
			return fmt.Errorf("stream.Send(): %v, want %v",err,errRemoteClosed)
		}
		return nil
	})
	if err!=nil { t.Fatal(err) }
}

func TestProtocol_TooLargeMsg(t *testing.T) {
	c1,c2 := testConn(t)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		kind := StreamKind(0)
		maxMsgSize := uint64(10)
		
		// Bad mux.
		badMux := NewMux(makeConfig(kind))
		s.SpawnBg(func() error { return ignoreDisconnect(badMux.Run(ctx,c1)) })
		s.SpawnBg(func() error {
			t.Log("Connect stream.")
			stream,err := badMux.Connect(ctx,kind,0,0)
			if err!=nil { return fmt.Errorf("mux2.Connect(): %w",err) }
			defer stream.Close()
			rng := utils.TestRng()
			badMsg := utils.GenBytes(rng,int(maxMsgSize)+1)
			t.Log("Malform stream state to allow too large messages.")
			for inner := range stream.state.inner.Lock() {
				inner.send.maxMsgSize = uint64(len(badMsg))
			}
			t.Log("Send too large message")
			return utils.IgnoreCancel(stream.Send(ctx,badMsg))
		})

		mux := NewMux(makeConfig(kind))
		s.SpawnBg(func() error {
			t.Log("Accept stream")
			stream,err := mux.Accept(ctx,kind,maxMsgSize,10)
			if err!=nil { return fmt.Errorf("mux.Accept(): %w",err) }
			stream.Close()
			return utils.IgnoreCancel(err)
		})
		err := mux.Run(ctx,c2)
		t.Logf("mux terminated: %v",err)
		if !errors.Is(err,errTooLargeMsg) {
			return fmt.Errorf("err = %v, want %v",err,errTooLargeMsg)
		}
		return nil
	})
	if err!=nil { t.Fatal(err) }
}

func TestProtocol_TooManyMsgs(t *testing.T) {
	c1,c2 := testConn(t)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		kind := StreamKind(0)
		maxMsgSize := uint64(10)
		window := uint64(3)
		
		// Bad mux.
		badMux := NewMux(makeConfig(kind))
		s.SpawnBg(func() error { return ignoreDisconnect(badMux.Run(ctx,c1)) })
		s.SpawnBg(func() error {
			t.Log("Connect stream.")
			stream,err := badMux.Connect(ctx,kind,0,0)
			if err!=nil { return fmt.Errorf("mux2.Connect(): %w",err) }
			defer stream.Close()
			t.Log("Malform stream state to allow too many messages.")
			for inner := range stream.state.inner.Lock() {
				inner.send.end = window+1 
			}
			t.Log("Send too many messages")
			rng := utils.TestRng()
			for range window {
				if err:=stream.Send(ctx,utils.GenBytes(rng,int(maxMsgSize))); err!=nil {
					return err
				}
			}
			return utils.IgnoreCancel(stream.Send(ctx,utils.GenBytes(rng,int(maxMsgSize))))
		})

		mux := NewMux(makeConfig(kind))
		s.SpawnBg(func() error {
			t.Log("Accept stream")
			stream,err := mux.Accept(ctx,kind,maxMsgSize,window)
			if err!=nil { return fmt.Errorf("mux.Accept(): %w",err) }
			stream.Close()
			return nil
		})
		err := mux.Run(ctx,c2)
		t.Logf("mux terminated: %v",err)
		if !errors.Is(err,errTooManyMsgs) {
			return fmt.Errorf("err = %v, want %v",err,errTooManyMsgs)
		}
		return nil
	})
	if err!=nil { t.Fatal(err) }
}

func TestProtocol_FrameAfterClose(t *testing.T) {
	c1,c2 := testConn(t)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		kind := StreamKind(0)
		
		// Bad mux.
		badMux := NewMux(makeConfig(kind))	
		maxMsgSize := uint64(10)
		s.SpawnBg(func() error { return ignoreDisconnect(badMux.Run(ctx,c1)) })
		s.SpawnBg(func() error {
			t.Log("Connect stream.")
			stream,err := badMux.Connect(ctx,kind,0,0)
			if err!=nil { return fmt.Errorf("mux2.Connect(): %w",err) }
			defer stream.Close()
			t.Log("Trigger sending CLOSED frame without actually closing.")
			for inner := range stream.state.inner.Lock() {
				stream.close(inner)
				inner.closed.local = false
			}
			t.Log("Send 2 messages.")
			// We need to send 2 messages, because CLOSE might get delivered together with the 1st message
			// in which case peer would not realize that they were supposed to be sent out of order.
			rng := utils.TestRng()
			for range 2 {
				if err:=stream.Send(ctx,utils.GenBytes(rng,int(maxMsgSize))); err!=nil {
					return utils.IgnoreCancel(err)
				}
			}
			return nil
			
		})

		mux := NewMux(makeConfig(kind))
		s.SpawnBg(func() error {
			t.Log("Accept stream")
			stream,err := mux.Accept(ctx,kind,maxMsgSize,10)
			if err!=nil { return fmt.Errorf("mux.Accept(): %w",err) }
			defer stream.Close()
			t.Log("Keep stream open")
			<-ctx.Done()
			return nil
		})
		err := mux.Run(ctx,c2)
		t.Logf("mux terminated: %v",err)
		if !errors.Is(err,errFrameAfterClose) {
			return fmt.Errorf("err = %v, want %v",err,errFrameAfterClose)
		}
		return nil
	})
	if err!=nil { t.Fatal(err) }
}

func TestProtocol_TooManyAccepts(t *testing.T) {
	c1,c2 := testConn(t)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		kind := StreamKind(0)
		
		// Bad mux.
		badMux := NewMux(makeConfig(kind))	
		s.SpawnBg(func() error { return ignoreDisconnect(badMux.Run(ctx,c1)) })
		s.SpawnBg(func() error {
			// Artificially connect too many streams.
			for queue,ctrl := range badMux.queue.Lock() {
				for i := range badMux.cfg.Kinds[kind].MaxConnects+1 {
					f := queue.Get(streamID(2*i))
					f.Header.Kind = utils.Alloc(uint64(kind))
				}
				ctrl.Updated()
			}
			return nil
		})

		mux := NewMux(makeConfig(kind))
		err := mux.Run(ctx,c2)
		t.Logf("mux terminated: %v",err)
		if !errors.Is(err,errTooManyAccepts) {
			return fmt.Errorf("err = %v, want %v",err,errTooManyAccepts)
		}
		return nil
	})
	if err!=nil { t.Fatal(err) }
}

func TestProtocol_UnknownStream(t *testing.T) {
	c1,c2 := testConn(t)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		kind := StreamKind(0)
		
		// Bad mux.
		badMux := NewMux(makeConfig(kind))	
		s.SpawnBg(func() error { return ignoreDisconnect(badMux.Run(ctx,c1)) })
		s.SpawnBg(func() error {
			// Artificially accept a stream without connect.
			for queue,ctrl := range badMux.queue.Lock() {
				f := queue.Get(streamID(1))
				f.Header.Kind = utils.Alloc(uint64(kind))
				ctrl.Updated()
			}
			return nil
		})

		mux := NewMux(makeConfig(kind))
		err := mux.Run(ctx,c2)
		t.Logf("mux terminated: %v",err)
		if !errors.Is(err,errUnknownStream) {
			return fmt.Errorf("err = %v, want %v",err,errUnknownStream)
		}
		return nil
	})
	if err!=nil { t.Fatal(err) }
}

func TestProtocol_UnknownKind(t *testing.T) {
	c1,c2 := testConn(t)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		kind := StreamKind(0)
		badKind := StreamKind(1)
		
		// Bad mux.
		badMux := NewMux(makeConfig(kind))	
		s.SpawnBg(func() error { return ignoreDisconnect(badMux.Run(ctx,c1)) })
		s.SpawnBg(func() error {
			// Artificially connect a stream of unknown kind. 
			for queue,ctrl := range badMux.queue.Lock() {
				f := queue.Get(streamID(0))
				f.Header.Kind = utils.Alloc(uint64(badKind))
				ctrl.Updated()
			}
			return nil
		})
		mux := NewMux(makeConfig(kind))
		err := mux.Run(ctx,c2)
		t.Logf("mux terminated: %v",err)
		// Unknown kinds are treated as if they has 0 allowed accepts.
		if !errors.Is(err,errTooManyAccepts) {
			return fmt.Errorf("err = %v, want %v",err,errTooManyAccepts)
		}
		return nil
	})
	if err!=nil { t.Fatal(err) }
}

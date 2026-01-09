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
	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/crypto/ed25519"
)

// happy path
//   SecretConnection underneath.
//   concurrent send/recv multiple concurrent streams
//   more streams than declared
//   streams not closed by each side.
//   send without recv (head of line blocking)
func TestHappyPath(t *testing.T) {
	rng := utils.TestRng()
	kindCount := 5
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		c1,c2 := tcp.TestPipe()
		s.SpawnBg(func() error {
			<-ctx.Done()
			c1.Close()
			c2.Close()
			return nil
		})
		s.Spawn(func() error {
			sc,err := conn.MakeSecretConnection(ctx,c1,ed25519.GenerateSecretKey())
			if err!=nil { return err }
			kinds := map[StreamKind]*StreamKindConfig {}
			for kind := range StreamKind(kindCount) {
				kinds[kind] = &StreamKindConfig {
					MaxAccepts: uint64(rng.Intn(4)+1),
					MaxConnects: uint64(rng.Intn(4)+1),
				}
			}
			mux := NewMux(&Config {FrameSize: 10 * 1024, Kinds: kinds})
			s.SpawnBg(func() error { return mux.Run(ctx,sc) })
			for kind,kindCfg := range kinds {
				s.SpawnBg(func() error {
					for {
						maxMsgSize := uint64(rng.Intn(10000)+100)
						window := uint64(rng.Intn(10)+1)
						stream,err := mux.Accept(ctx,kind,maxMsgSize,window)
						if err!=nil {
							return utils.IgnoreCancel(err)
						}
						s.Spawn(func() error {
							defer stream.Close()
							for {
								msg,err := stream.Recv(ctx, true)
								if err!=nil {
									if errors.Is(err, errClosed) {
										return nil
									}
									return fmt.Errorf("stream.Recv(): %w",err)
								}
								// transform
								if err:=stream.Send(ctx,msg); err!=nil {
									return fmt.Errorf("stream.Send(): %w",err)
								}
							}
						})
					}
				})
				var count atomic.Int64
				for range 20 {
					s.Spawn(func() error {
						maxMsgSize := uint64(rng.Intn(10000)+100)
						window := uint64(rng.Intn(10)+1)
						stream,err := mux.Connect(ctx,kind,maxMsgSize,window)
						if err!=nil { return fmt.Errorf("mux.Connect(): %w",err) }
						if got,wantMax := uint64(count.Add(1)),kindCfg.MaxConnects; got>wantMax {
							return fmt.Errorf("got %v concurrent connects, want <= %v",got,wantMax)
						}
						defer stream.Close()
						defer count.Add(-1)
						var want [][]byte
						for range rng.Intn(100) {
							size := rng.Intn(int(min(maxMsgSize,stream.MaxMsgSize())))
							want = append(want,utils.GenBytes(rng,size))
						}
						s.Spawn(func() error {
							for _,msg := range want {
								if err := stream.Send(ctx,msg); err!=nil {
									return fmt.Errorf("stream.Send(): %w",err)
								}
							}
							return nil
						})
						for _,wantMsg := range want {
							gotMsg,err := stream.Recv(ctx, true)
							if err!=nil { return fmt.Errorf("stream.Recv(): %w",err) }
							// TODO: use nontrivial transformation on the server side
							// to ensure that mux does not have pathological impl.
							if err:=utils.TestDiff(wantMsg,gotMsg); err!=nil {
								return err
							}
						}
						return nil
					})
				}
			}
			return nil
		})
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

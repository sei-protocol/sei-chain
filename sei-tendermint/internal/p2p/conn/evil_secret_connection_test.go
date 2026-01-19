package conn

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/tendermint/tendermint/internal/protoutils"
	"github.com/tendermint/tendermint/internal/p2p/pb"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/libs/utils/scope"
)

func runEvilConn(ctx context.Context, conn Conn, shareEphKey, badEphKey bool) error {
	return utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			var buf [1024]byte
			for {
				if err := conn.Read(ctx,buf[:]); err!=nil {
					return err
				}
			}
		})
		// ephemeral key exchange.
		if !shareEphKey {
			return nil
		}
		loc := genEphKey()
		ephKey := loc.public[:]
		if badEphKey {
			ephKey = []byte("drop users;")
		}
		ephKeyMsg := &pb.Preface{StsPublicKey: ephKey}
		if err:=WriteSizedMsg(ctx,conn,protoutils.Marshal(ephKeyMsg)); err!=nil {
			return fmt.Errorf("WriteSizedMsg(): %w",err)
		}
		return conn.Flush(ctx)
	}))
}

// TestMakeSecretConnection creates an evil connection and tests that
// MakeSecretConnection errors at different stages.
func TestMakeSecretConnection(t *testing.T) {
	testCases := []struct {
		name string
		shareEphKey bool
		badEphKey bool
		err  utils.Option[error]
	}{
		{"all good", true,false, utils.None[error]()},
		{"share bad ephemeral key", true, true, utils.Some(errDH)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
				c1,c2 := tcp.TestPipe()
				s.SpawnBg(func() error {
					_ = c1.Run(ctx)
					return nil
				})
				s.SpawnBg(func() error {
					_ = c2.Run(ctx)
					return nil
				})

				s.SpawnBg(func() error { return runEvilConn(ctx,c2,tc.shareEphKey,tc.badEphKey) })
				_, err := MakeSecretConnection(ctx, c1)
				if wantErr, ok := tc.err.Get(); ok {
					if !errors.Is(err, wantErr) {
						return fmt.Errorf("got %v, want %v", err, wantErr)
					}
					return nil
				} else {
					return err
				}
			})
			require.NoError(t, err)
		})
	}
}

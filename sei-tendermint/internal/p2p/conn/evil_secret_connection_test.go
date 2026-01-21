package conn

import (
	"context"
	"fmt"
	"testing"

	"github.com/tendermint/tendermint/internal/p2p/pb"
	"github.com/tendermint/tendermint/internal/protoutils"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
)


func TestLowOrderPoint(t *testing.T) {
	_, err := newSecretConnection(nil, genEphKey(), ephPublic{})
	require.True(t, errors.Is(err, errDH))
}

func runEvilConn(ctx context.Context, conn Conn) error {
	return utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			var buf [1024]byte
			for {
				if err := conn.Read(ctx, buf[:]); err != nil {
					return err
				}
			}
		})
		ephKey := []byte("drop users;")
		ephKeyMsg := &pb.Preface{StsPublicKey: ephKey}
		if err := WriteSizedMsg(ctx, conn, protoutils.Marshal(ephKeyMsg)); err != nil {
			return fmt.Errorf("WriteSizedMsg(): %w", err)
		}
		return conn.Flush(ctx)
	}))
}

// TestMakeSecretConnection creates an evil connection and tests that
// MakeSecretConnection errors at different stages.
func TestBadEphemeralKey(t *testing.T) {
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		c1, c2 := tcp.TestPipe()
		s.SpawnBg(func() error {
			_ = c1.Run(ctx)
			return nil
		})
		s.SpawnBg(func() error {
			_ = c2.Run(ctx)
			return nil
		})

		s.SpawnBg(func() error { return runEvilConn(ctx, c2) })
		_, err := MakeSecretConnection(ctx, c1)
		if utils.IgnoreCancel(err) == nil {
			return fmt.Errorf("got %v want, key exchange failure", err)
		}
		return nil
	})
	require.NoError(t, err)
}

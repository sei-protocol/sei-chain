package conn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	gogotypes "github.com/gogo/protobuf/types"

	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/internal/libs/protoio"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/libs/utils/scope"
)

func TestLowOrderPoint(t *testing.T) {
	_, err := newSecretConnection(nil, genEphKey(), ephPublic{})
	require.True(t, errors.Is(err, errDH))
}

type evilConn struct {
	net.Conn
	remEphKey utils.AtomicSend[utils.Option[ephPublic]]
	reader    *io.PipeReader
	writer    *io.PipeWriter

	shareEphKey        bool
	badEphKey          bool
	shareAuthSignature bool
	badAuthSignature   bool
}

func newEvilConn(shareEphKey, badEphKey, shareAuthSignature, badAuthSignature bool) *evilConn {
	r, w := io.Pipe()
	return &evilConn{
		remEphKey:          utils.NewAtomicSend(utils.None[ephPublic]()),
		reader:             r,
		writer:             w,
		shareEphKey:        shareEphKey,
		badEphKey:          badEphKey,
		shareAuthSignature: shareAuthSignature,
		badAuthSignature:   badAuthSignature,
	}
}

type WriterConn struct {
	net.Conn
	writer io.Writer
}

func (wc WriterConn) Write(data []byte) (int, error) { return wc.writer.Write(data) }

func (c *evilConn) Run(ctx context.Context) error {
	defer c.writer.Close()

	// ephemeral key exchange.
	if !c.shareEphKey {
		return nil
	}
	loc := genEphKey()
	ephKey := loc.public
	if c.badEphKey {
		ephKey = ephPublic{}
	}
	ephKeyMsg := &gogotypes.BytesValue{Value: ephKey[:]}
	if _, err := c.writer.Write(utils.OrPanic1(protoio.MarshalDelimited(ephKeyMsg))); err != nil {
		return err
	}

	// authorisation signature exchange.
	if !c.shareAuthSignature {
		return nil
	}
	rem, err := c.remEphKey.Wait(ctx, func(k utils.Option[ephPublic]) bool { return k.IsPresent() })
	if err != nil {
		return err
	}
	sc := utils.OrPanic1(newSecretConnection(WriterConn{writer: c.writer}, loc, rem.OrPanic()))
	challenge := sc.challenge[:]
	if c.badAuthSignature {
		challenge = []byte("invalid challenge")
	}
	k := ed25519.GenerateSecretKey()
	authSigMsg := authSigMessageConv.Encode(&authSigMessage{Key: k.Public(), Sig: k.Sign(challenge)})
	if _, err := sc.Write(utils.OrPanic1(protoio.MarshalDelimited(authSigMsg))); err != nil {
		return err
	}
	return sc.Flush()
}

func (c *evilConn) Read(data []byte) (n int, err error) {
	return c.reader.Read(data)
}

func (c *evilConn) Write(data []byte) (n int, err error) {
	// Fetch ephemeral key then ignore the rest of the stream.
	if c.remEphKey.Load().IsPresent() {
		return len(data), nil
	}
	var bytes gogotypes.BytesValue
	utils.OrPanic(protoio.UnmarshalDelimited(data, &bytes))
	c.remEphKey.Store(utils.Some(ephPublic(bytes.Value)))
	return len(data), nil
}

// TestMakeSecretConnection creates an evil connection and tests that
// MakeSecretConnection errors at different stages.
func TestMakeSecretConnection(t *testing.T) {
	testCases := []struct {
		name string
		conn *evilConn
		err  utils.Option[error]
	}{
		{"refuse to share ephemeral key", newEvilConn(false, false, false, false), utils.Some(errDH)},
		{"share bad ephemeral key", newEvilConn(true, true, false, false), utils.Some(errDH)},
		{"refuse to share auth signature", newEvilConn(true, false, false, false), utils.Some(errAuth)},
		{"share bad auth signature", newEvilConn(true, false, true, true), utils.Some(errAuth)},
		{"all good", newEvilConn(true, false, true, false), utils.None[error]()},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
				s.SpawnBg(func() error { return tc.conn.Run(ctx) })
				privKey := ed25519.GenerateSecretKey()
				_, err := MakeSecretConnection(ctx, tc.conn, privKey)
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

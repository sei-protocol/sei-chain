package conn

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"

	gogotypes "github.com/gogo/protobuf/types"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/internal/libs/protoio"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	tmp2p "github.com/tendermint/tendermint/proto/tendermint/p2p"
)

type buffer struct {
	net.Conn
	next bytes.Buffer
}

func (b *buffer) Read(data []byte) (n int, err error) {
	return b.next.Read(data)
}

func (b *buffer) Write(data []byte) (n int, err error) {
	return b.next.Write(data)
}

func (b *buffer) Bytes() []byte {
	return b.next.Bytes()
}

func (b *buffer) Close() error {
	return nil
}

type evilConn struct {
	net.Conn
	secretConn *SecretConnection
	buffer     *buffer

	loc     ephSecret
	rem     ephPublic
	privKey crypto.PrivKey

	readStep   int
	writeStep  int
	readOffset int

	shareEphKey        bool
	badEphKey          bool
	shareAuthSignature bool
	badAuthSignature   bool
}

func newEvilConn(shareEphKey, badEphKey, shareAuthSignature, badAuthSignature bool) *evilConn {
	return &evilConn{
		loc:                genEphKey(),
		privKey:            ed25519.GenerateSecretKey(),
		shareEphKey:        shareEphKey,
		badEphKey:          badEphKey,
		shareAuthSignature: shareAuthSignature,
		badAuthSignature:   badAuthSignature,
	}
}

func (c *evilConn) Read(data []byte) (n int, err error) {
	if !c.shareEphKey {
		return 0, io.EOF
	}

	switch c.readStep {
	case 0:
		if !c.badEphKey {
			bz, err := protoio.MarshalDelimited(&gogotypes.BytesValue{Value: c.loc.public[:]})
			if err != nil {
				panic(err)
			}
			copy(data, bz[c.readOffset:])
			n = len(data)
		} else {
			bz, err := protoio.MarshalDelimited(&gogotypes.BytesValue{Value: []byte("drop users;")})
			if err != nil {
				panic(err)
			}
			copy(data, bz)
			n = len(data)
		}
		c.readOffset += n

		if n >= 32 {
			c.readOffset = 0
			c.readStep = 1
			if !c.shareAuthSignature {
				c.readStep = 2
			}
		}

		return n, nil
	case 1:
		signature := c.signChallenge()
		if !c.badAuthSignature {
			pkpb := crypto.PubKeyConv.Encode(c.privKey.Public())
			bz, err := protoio.MarshalDelimited(&tmp2p.AuthSigMessage{PubKey: pkpb, Sig: signature.Bytes()})
			if err != nil {
				panic(err)
			}
			n, err = c.secretConn.Write(bz)
			if err != nil {
				panic(err)
			}
			if c.readOffset > len(c.buffer.Bytes()) {
				return len(data), nil
			}
			copy(data, c.buffer.Bytes()[c.readOffset:])
		} else {
			bz, err := protoio.MarshalDelimited(&gogotypes.BytesValue{Value: []byte("select * from users;")})
			if err != nil {
				panic(err)
			}
			n, err = c.secretConn.Write(bz)
			if err != nil {
				panic(err)
			}
			if c.readOffset > len(c.buffer.Bytes()) {
				return len(data), nil
			}
			copy(data, c.buffer.Bytes())
		}
		c.readOffset += len(data)
		return n, nil
	default:
		return 0, io.EOF
	}
}

func (c *evilConn) Write(data []byte) (n int, err error) {
	switch c.writeStep {
	case 0:
		var bytes gogotypes.BytesValue
		utils.OrPanic(protoio.UnmarshalDelimited(data, &bytes))
		c.rem = ephPublic(bytes.Value)
		c.writeStep = 1
		if !c.shareAuthSignature {
			c.writeStep = 2
		}
		return len(data), nil
	case 1:
		// Signature is not needed, therefore skipped.
		return len(data), nil
	default:
		return 0, io.EOF
	}
}

func (c *evilConn) Close() error {
	return nil
}

func (c *evilConn) signChallenge() ed25519.Signature {
	b := &buffer{}
	c.secretConn = newSecretConnection(b, c.loc, c.rem)
	c.buffer = b
	return c.privKey.Sign(c.secretConn.challenge[:])
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
			ctx := t.Context()
			privKey := ed25519.GenerateSecretKey()
			_, err := MakeSecretConnection(ctx, tc.conn, privKey)
			if wantErr, ok := tc.err.Get(); ok {
				require.True(t, errors.Is(err, wantErr), "got %v, want %v", err, wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

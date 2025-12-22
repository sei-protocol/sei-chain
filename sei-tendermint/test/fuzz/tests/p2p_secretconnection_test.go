//go:build gofuzz || go1.18

package tests

import (
	"bytes"
	"fmt"
	"io"
	"context"
	"testing"
	"net"

	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/require"
)

func FuzzP2PSecretConnection(f *testing.F) {
	f.Fuzz(fuzz)
}

func fuzz(t *testing.T, data []byte) {
	if len(data) == 0 {
		return
	}

	fooConn, barConn := makeSecretConnPair(t)

	// Run Write in a separate goroutine because if data is greater than 1024
	// bytes, each Write must be followed by Read (see io.Pipe documentation).
	go func() {
		// Copy data because Write modifies the slice.
		dataToWrite := make([]byte, len(data))
		copy(dataToWrite, data)

		n, err := fooConn.Write(dataToWrite)
		if err != nil {
			panic(err)
		}
		if n < len(data) {
			panic(fmt.Sprintf("wanted to write %d bytes, but %d was written", len(data), n))
		}
	}()

	dataRead := make([]byte, len(data))
	totalRead := 0
	for totalRead < len(data) {
		buf := make([]byte, len(data)-totalRead)
		m, err := barConn.Read(buf)
		if err != nil {
			panic(err)
		}
		copy(dataRead[totalRead:], buf[:m])
		totalRead += m
	}

	if !bytes.Equal(data, dataRead) {
		panic("bytes written != read")
	}
}

type kvstoreConn struct {
	net.Conn
	reader *io.PipeReader
	writer *io.PipeWriter
}

func (drw kvstoreConn) Read(data []byte) (n int, err error)  { return drw.reader.Read(data) }
func (drw kvstoreConn) Write(data []byte) (n int, err error) { return drw.writer.Write(data) }

func (drw kvstoreConn) Close() (err error) {
	err2 := drw.writer.CloseWithError(io.EOF)
	err1 := drw.reader.Close()
	if err2 != nil {
		return err
	}
	return err1
}



// Each returned ReadWriteCloser is akin to a net.Connection
func makeKVStoreConnPair() (fooConn, barConn kvstoreConn) {
	barReader, fooWriter := io.Pipe()
	fooReader, barWriter := io.Pipe()
	return kvstoreConn{reader: fooReader, writer: fooWriter}, kvstoreConn{reader: barReader, writer: barWriter}
}

func makeSecretConnPair(tb testing.TB) (sc1 *conn.SecretConnection, sc2 *conn.SecretConnection) {
	ctx := tb.Context()
	c1, c2 := makeKVStoreConnPair()
	k1 := ed25519.GenerateSecretKey()
	k2 := ed25519.GenerateSecretKey()

	// Make connections from both sides in parallel.
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			var err error
			sc1, err = conn.MakeSecretConnection(ctx, c1, k1)
			return err
		})
		s.Spawn(func() error {
			var err error
			sc2, err = conn.MakeSecretConnection(ctx, c2, k2)
			return err
		})
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	require.Equal(tb, k1.Public(), sc2.RemotePubKey())
	require.Equal(tb, k2.Public(), sc1.RemotePubKey())
	return sc1, sc2
}

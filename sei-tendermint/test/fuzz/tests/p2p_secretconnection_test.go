//go:build gofuzz

package tests

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"

	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
)

func FuzzP2PSecretConnection(f *testing.F) {
	f.Fuzz(fuzz)
}

func fuzz(t *testing.T, data []byte) {
	ctx := t.Context()
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
		utils.OrPanic(fooConn.Write(ctx, dataToWrite))
		utils.OrPanic(fooConn.Flush(ctx))
	}()

	dataRead := make([]byte, len(data))
	utils.OrPanic(barConn.Read(ctx, dataRead))

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

func spawnBgForTest(t testing.TB, task func(context.Context) error) {
	go func() {
		if err := task(t.Context()); t.Context().Err() == nil {
			utils.OrPanic(err)
		}
	}()
}

func makeSecretConnPair(tb testing.TB) (sc1 *conn.SecretConnection, sc2 *conn.SecretConnection) {
	ctx := tb.Context()
	c1, c2 := tcp.TestPipe()
	spawnBgForTest(tb, c1.Run)
	spawnBgForTest(tb, c2.Run)
	// Make connections from both sides in parallel.
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			var err error
			sc1, err = conn.MakeSecretConnection(ctx, c1)
			return err
		})
		s.Spawn(func() error {
			var err error
			sc2, err = conn.MakeSecretConnection(ctx, c2)
			return err
		})
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	return sc1, sc2
}

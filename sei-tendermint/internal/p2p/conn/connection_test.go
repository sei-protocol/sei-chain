package conn

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/assert"

	"github.com/tendermint/tendermint/internal/libs/protoio"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils/require"
	tmp2p "github.com/tendermint/tendermint/proto/tendermint/p2p"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

const maxPingPongPacketSize = 1024 // bytes

func spawnMConnection(
	logger log.Logger,
	conn net.Conn,
) *MConnection {
	chDescs := []*ChannelDescriptor{{ID: 0x01, Priority: 1, SendQueueCapacity: 1}}
	return spawnMConnectionWithCh(logger, conn, chDescs)
}

func spawnMConnectionWithCh(
	logger log.Logger,
	conn net.Conn,
	chDescs []*ChannelDescriptor,
) *MConnection {
	cfg := DefaultMConnConfig()
	cfg.PingInterval = 250 * time.Millisecond
	cfg.PongTimeout = 500 * time.Millisecond
	return SpawnMConnection(logger, conn, chDescs, cfg)
}

func TestMConnectionSendFlushStop(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(closeAll(t, client, server))

	ctx := t.Context()

	clientConn := spawnMConnection(log.NewNopLogger(), client)
	defer clientConn.Close()

	msg := []byte("abc")
	assert.NoError(t, clientConn.Send(ctx, 0x01, msg))

	msgLength := 14

	// start the reader in a new routine, so we can flush
	errCh := make(chan error)
	go func() {
		msgB := make([]byte, msgLength)
		_, err := server.Read(msgB)
		if err != nil {
			t.Error(err)
			return
		}
		errCh <- err
	}()

	timer := time.NewTimer(3 * time.Second)
	select {
	case <-errCh:
	case <-timer.C:
		t.Error("timed out waiting for msgs to be read")
	}
}

func TestMConnectionSend(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(closeAll(t, client, server))

	ctx := t.Context()

	mconn := spawnMConnection(log.NewNopLogger(), client)
	defer mconn.Close()

	msg := []byte("Ant-Man")
	assert.NoError(t, mconn.Send(ctx, 0x01, msg))
	// Note: subsequent Send/TrySend calls could pass because we are reading from
	// the send queue in a separate goroutine.
	_, err := server.Read(make([]byte, len(msg)))
	if err != nil {
		t.Error(err)
	}

	msg = []byte("Spider-Man")
	assert.NoError(t, mconn.Send(ctx, 0x01, msg))
	_, err = server.Read(make([]byte, len(msg)))
	if err != nil {
		t.Error(err)
	}

	assert.Error(t, mconn.Send(ctx, 0x05, []byte("Absorbing Man")), "Send should fail because channel is unknown")
}

func TestMConnectionReceive(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(closeAll(t, client, server))

	logger := log.NewNopLogger()

	ctx := t.Context()
	mconn1 := spawnMConnection(logger, client)
	defer mconn1.Close()
	mconn2 := spawnMConnection(logger, server)
	defer mconn2.Close()

	msg := []byte("Cyclops")
	assert.NoError(t, mconn2.Send(ctx, 0x01, msg))
	_, receivedBytes, err := mconn1.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, msg, receivedBytes)
}

func TestMConnectionWillEventuallyTimeout(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(closeAll(t, client, server))
	ctx := t.Context()
	mconn := spawnMConnection(log.NewNopLogger(), client)
	defer mconn.Close()
	go func() {
		// read the send buffer so that the send receive
		// doesn't get blocked.
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				_, _ = io.ReadAll(server)
			case <-ctx.Done():
				return
			}
		}
	}()
	<-mconn.handle.Done()
	if got, want := mconn.handle.Err(), errPongTimeout; !errors.Is(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestMConnectionMultiplePongsInTheBeginning(t *testing.T) {
	ctx := t.Context()
	server, client := net.Pipe()
	t.Cleanup(closeAll(t, client, server))
	mconn := spawnMConnection(log.NewNopLogger(), client)
	defer mconn.Close()

	// sending 3 pongs in a row (abuse)
	protoWriter := protoio.NewDelimitedWriter(server)

	_, err := protoWriter.WriteMsg(mustWrapPacket(&tmp2p.PacketPong{}))
	require.NoError(t, err)

	_, err = protoWriter.WriteMsg(mustWrapPacket(&tmp2p.PacketPong{}))
	require.NoError(t, err)

	_, err = protoWriter.WriteMsg(mustWrapPacket(&tmp2p.PacketPong{}))
	require.NoError(t, err)

	// read ping (one byte)
	var packet tmp2p.Packet
	_, err = protoio.NewDelimitedReader(server, maxPingPongPacketSize).ReadMsg(&packet)
	require.NoError(t, err)

	// respond with pong
	_, err = protoWriter.WriteMsg(mustWrapPacket(&tmp2p.PacketPong{}))
	require.NoError(t, err)

	pongTimerExpired := mconn.config.PongTimeout + 20*time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, pongTimerExpired)
	defer cancel()
	_, _, err = mconn.Recv(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
}

func TestMConnectionMultiplePings(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(closeAll(t, client, server))

	mconn := spawnMConnection(log.NewNopLogger(), client)
	defer mconn.Close()

	// sending 3 pings in a row (abuse)
	// see https://github.com/tendermint/tendermint/issues/1190
	protoReader := protoio.NewDelimitedReader(server, maxPingPongPacketSize)
	protoWriter := protoio.NewDelimitedWriter(server)
	var pkt tmp2p.Packet

	_, err := protoWriter.WriteMsg(mustWrapPacket(&tmp2p.PacketPing{}))
	require.NoError(t, err)

	_, err = protoReader.ReadMsg(&pkt)
	require.NoError(t, err)

	_, err = protoWriter.WriteMsg(mustWrapPacket(&tmp2p.PacketPing{}))
	require.NoError(t, err)

	_, err = protoReader.ReadMsg(&pkt)
	require.NoError(t, err)

	_, err = protoWriter.WriteMsg(mustWrapPacket(&tmp2p.PacketPing{}))
	require.NoError(t, err)

	_, err = protoReader.ReadMsg(&pkt)
	require.NoError(t, err)

	require.NoError(t, mconn.handle.Err())
}

func TestMConnectionPingPongs(t *testing.T) {
	// check that we are not leaking any go-routines
	t.Cleanup(leaktest.CheckTimeout(t, 10*time.Second))

	server, client := net.Pipe()
	t.Cleanup(closeAll(t, client, server))

	ctx := t.Context()

	mconn := spawnMConnection(log.NewNopLogger(), client)
	defer mconn.Close()

	protoReader := protoio.NewDelimitedReader(server, maxPingPongPacketSize)
	protoWriter := protoio.NewDelimitedWriter(server)
	var pkt tmp2p.PacketPing

	// read ping
	_, err := protoReader.ReadMsg(&pkt)
	require.NoError(t, err)

	// respond with pong
	_, err = protoWriter.WriteMsg(mustWrapPacket(&tmp2p.PacketPong{}))
	require.NoError(t, err)

	time.Sleep(mconn.config.PingInterval)

	// read ping
	_, err = protoReader.ReadMsg(&pkt)
	require.NoError(t, err)

	// respond with pong
	_, err = protoWriter.WriteMsg(mustWrapPacket(&tmp2p.PacketPong{}))
	require.NoError(t, err)

	pongTimerExpired := mconn.config.PongTimeout + 20*time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, pongTimerExpired)
	defer cancel()
	_, _, err = mconn.Recv(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
}

func TestMConnectionStopsAndReturnsError(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(closeAll(t, client, server))
	ctx := t.Context()

	mconn := spawnMConnection(log.NewNopLogger(), client)
	defer mconn.Close()

	if err := client.Close(); err != nil {
		t.Error(err)
	}

	// TODO(gprusak): proto reader does not wrap the error when cannot read the next byte,
	// and the actual error depends on the underlying connection (EOF, ErrClosedPipe, etc),
	// hence we cannot distinguish the EOF from malformed message. Fix the proto reader.
	var want errBadEncoding
	if _, _, got := mconn.Recv(ctx); !errors.As(got, &want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func newConns(
	t *testing.T,
) (*MConnection, *MConnection) {
	server, client := net.Pipe()
	// create client conn with two channels
	chDescs := []*ChannelDescriptor{
		{ID: 0x01, Priority: 1, SendQueueCapacity: 1},
		{ID: 0x02, Priority: 1, SendQueueCapacity: 1},
	}
	logger := log.NewNopLogger()
	mc := spawnMConnectionWithCh(logger, client, chDescs)
	t.Cleanup(func() { mc.Close() })
	ms := spawnMConnection(logger, server)
	t.Cleanup(func() { ms.Close() })
	return mc, ms
}

func TestMConnectionReadErrorBadEncoding(t *testing.T) {
	ctx := t.Context()
	mconnClient, mconnServer := newConns(t)

	// Write it.
	_, err := mconnClient.conn.Write([]byte{1, 2, 3, 4, 5})
	require.NoError(t, err)
	var want errBadEncoding
	if _, _, err := mconnServer.Recv(ctx); !errors.As(err, &want) {
		t.Fatalf("expected errBadEncoding, got %v", err)
	}
}

func TestMConnectionReadErrorUnknownChannel(t *testing.T) {
	ctx := t.Context()

	mconnClient, mconnServer := newConns(t)

	msg := []byte("Ant-Man")

	// fail to send msg on channel unknown by client
	if got, want := mconnClient.Send(ctx, 0x04, msg), (errBadChannel{}); !errors.As(got, &want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// send msg on channel unknown by the server.
	// should cause an error
	assert.NoError(t, mconnClient.Send(ctx, 0x02, msg))
	var want errBadChannel
	if _, _, got := mconnServer.Recv(ctx); !errors.As(got, &want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestMConnectionReadErrorLongMessage(t *testing.T) {
	ctx := t.Context()
	mconnClient, mconnServer := newConns(t)
	protoWriter := protoio.NewDelimitedWriter(mconnClient.conn)

	// send msg thats just right
	var packet = tmp2p.PacketMsg{
		ChannelID: 0x01,
		EOF:       true,
		Data:      make([]byte, mconnClient.config.MaxPacketMsgPayloadSize),
	}

	_, err := protoWriter.WriteMsg(mustWrapPacket(&packet))
	require.NoError(t, err)
	chID, got, err := mconnServer.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, ChannelID(0x01), chID)
	require.True(t, bytes.Equal(got, packet.Data))

	// send msg thats too long
	packet = tmp2p.PacketMsg{
		ChannelID: 0x01,
		EOF:       true,
		Data:      make([]byte, mconnClient.config.MaxPacketMsgPayloadSize+100),
	}

	// Depending on when the server will terminate the connection,
	// writing may fail or succeed.
	_, _ = protoWriter.WriteMsg(mustWrapPacket(&packet))
	var want errBadEncoding
	if _, _, err := mconnServer.Recv(ctx); !errors.As(err, &want) {
		t.Fatalf("expected errBadEncoding, got %v", err)
	}
}

func TestMConnectionReadErrorUnknownMsgType(t *testing.T) {
	ctx := t.Context()
	mconnClient, mconnServer := newConns(t)

	// send msg with unknown msg type
	_, err := protoio.NewDelimitedWriter(mconnClient.conn).WriteMsg(&types.Header{ChainID: "x"})
	require.NoError(t, err)
	var want errBadEncoding
	if _, _, got := mconnServer.Recv(ctx); !errors.As(got, &want) {
		t.Fatalf("expected errBadEncoding, got %v", got)
	}
}

func TestConnVectors(t *testing.T) {
	testCases := []struct {
		testName string
		msg      proto.Message
		expBytes string
	}{
		{"PacketPing", &tmp2p.PacketPing{}, "0a00"},
		{"PacketPong", &tmp2p.PacketPong{}, "1200"},
		{"PacketMsg", &tmp2p.PacketMsg{ChannelID: 1, EOF: false, Data: []byte("data transmitted over the wire")}, "1a2208011a1e64617461207472616e736d6974746564206f766572207468652077697265"},
	}

	for _, tc := range testCases {
		pm := mustWrapPacket(tc.msg)
		bz, err := pm.Marshal()
		require.NoError(t, err, tc.testName)
		require.Equal(t, tc.expBytes, hex.EncodeToString(bz), tc.testName)
	}
}

func TestMConnectionChannelOverflow(t *testing.T) {
	ctx := t.Context()
	m1, m2 := newConns(t)
	protoWriter := protoio.NewDelimitedWriter(m1.conn)

	var packet = tmp2p.PacketMsg{
		ChannelID: 0x01,
		EOF:       true,
		Data:      []byte(`42`),
	}
	_, err := protoWriter.WriteMsg(mustWrapPacket(&packet))
	require.NoError(t, err)
	_, _, err = m2.Recv(ctx)
	require.NoError(t, err)
	packet.ChannelID = int32(1025)
	_, err = protoWriter.WriteMsg(mustWrapPacket(&packet))
	require.NoError(t, err)
	_, _, err = m2.Recv(ctx)
	var want errBadChannel
	if !errors.As(err, &want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}

type closer interface {
	Close() error
}

func closeAll(t *testing.T, closers ...closer) func() {
	return func() {
		for _, s := range closers {
			if err := s.Close(); err != nil {
				t.Log(err)
			}
		}
	}
}

package conn

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/assert"

	"github.com/tendermint/tendermint/internal/libs/protoio"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/proto/tendermint/p2p"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

const maxPingPongPacketSize = 1024 // bytes

func spawnMConnection(
	t *testing.T,
	conn net.Conn,
) *MConnection {
	chDescs := []*ChannelDescriptor{{ID: 0x01, Priority: 1, SendQueueCapacity: 1}}
	return spawnMConnectionWithCh(t, conn, chDescs)

}

func spawnMConnectionWithCh(
	t *testing.T,
	conn net.Conn,
	chDescs []*ChannelDescriptor,
) *MConnection {
	cfg := DefaultMConnConfig()
	cfg.PingInterval = 250 * time.Millisecond
	cfg.PongTimeout = 500 * time.Millisecond
	c := SpawnMConnection(log.NewNopLogger(), conn, chDescs, cfg)
	t.Cleanup(func() { c.Close() })
	return c
}

func TestMConnectionSend(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(closeAll(t, client, server))
	ctx := t.Context()
	mconn := spawnMConnection(t, client)

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

func TestMConnectionSendRecv(t *testing.T) {
	t.Cleanup(leaktest.CheckTimeout(t, 10*time.Second))
	server, client := net.Pipe()
	t.Cleanup(closeAll(t, client, server))

	ctx := t.Context()
	mconn1 := spawnMConnection(t, client)
	mconn2 := spawnMConnection(t, server)

	msg := []byte("Cyclops")
	assert.NoError(t, mconn2.Send(ctx, 0x01, msg))
	_, receivedBytes, err := mconn1.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, msg, receivedBytes)

	// Close mconn1, should terminate gracefully.
	if err := utils.IgnoreCancel(mconn1.Close()); err != nil {
		t.Fatal(err)
	}
	// mconn2 should fail, because mconn1 closed.
	got, err := mconn2.handle.Join(ctx)
	require.NoError(t, err)
	require.Error(t, utils.IgnoreCancel(got))
}

func pingMsg() *p2p.Packet {
	return &p2p.Packet{
		Sum: &p2p.Packet_PacketPing{
			PacketPing: &p2p.PacketPing{},
		},
	}
}

func pongMsg() *p2p.Packet {
	return &p2p.Packet{
		Sum: &p2p.Packet_PacketPong{
			PacketPong: &p2p.PacketPong{},
		},
	}
}

func TestMConnectionPingPong(t *testing.T) {
	t.Cleanup(leaktest.CheckTimeout(t, 10*time.Second))
	server, client := net.Pipe()
	t.Cleanup(closeAll(t, client, server))
	mconn := spawnMConnection(t, client)

	protoReader := protoio.NewDelimitedReader(server, maxPingPongPacketSize)
	protoWriter := protoio.NewDelimitedWriter(server)
	for range 3 {
		// read ping
		var got p2p.Packet
		_, err := protoReader.ReadMsg(&got)
		require.NoError(t, err)
		if _, ok := got.Sum.(*p2p.Packet_PacketPing); !ok {
			t.Fatalf("expected ping, got %T", got.Sum)
		}

		// respond with pong
		_, err = protoWriter.WriteMsg(pongMsg())
		require.NoError(t, err)
	}

	// Read until connection dies.
	_, _ = io.ReadAll(server)
	// Expect pong timeout.
	if got := mconn.Close(); !errors.Is(got, errPongTimeout) {
		t.Fatalf("expected pong timeout error, got %v", got)
	}
}

func newConns(t *testing.T) (*MConnection, *MConnection) {
	server, client := net.Pipe()
	// create client conn with two channels
	chDescs := []*ChannelDescriptor{
		{ID: 0x01, Priority: 1, SendQueueCapacity: 1},
		{ID: 0x02, Priority: 1, SendQueueCapacity: 1},
	}
	mc := spawnMConnectionWithCh(t, client, chDescs)
	ms := spawnMConnection(t, server)
	return mc, ms
}

func TestMConnectionReadErrorBadEncoding(t *testing.T) {
	ctx := t.Context()
	mconnClient, mconnServer := newConns(t)

	// Write it.
	_, err := mconnClient.conn.Write([]byte{1, 2, 3, 4, 5})
	require.NoError(t, err)
	got, err := mconnServer.handle.Join(ctx)
	require.NoError(t, err)
	if want := (errBadEncoding{}); !errors.As(got, &want) {
		t.Fatalf("got %v, want %T", got, want)
	}
}

func TestMConnectionReadErrorUnknownChannel(t *testing.T) {
	ctx := t.Context()

	mconnClient, mconnServer := newConns(t)

	msg := []byte("Ant-Man")

	// fail to send msg on channel unknown by client
	if got, want := mconnClient.Send(ctx, 0x04, msg), (errBadChannel{}); !errors.As(got, &want) {
		t.Fatalf("got %v, want %T", got, want)
	}
	// send msg on channel unknown by the server.
	// should cause an error
	require.NoError(t, mconnClient.Send(ctx, 0x02, msg))
	got, err := mconnServer.handle.Join(ctx)
	require.NoError(t, err)
	if want := (errBadChannel{}); !errors.As(got, &want) {
		t.Fatalf("got %v, want %T", got, want)
	}
}

func TestMConnectionReadErrorLongMessage(t *testing.T) {
	ctx := t.Context()
	mconnClient, mconnServer := newConns(t)
	protoWriter := protoio.NewDelimitedWriter(mconnClient.conn)

	// send msg thats just right
	msg := &p2p.PacketMsg{
		ChannelID: 0x01,
		EOF:       true,
		Data:      make([]byte, mconnClient.config.MaxPacketMsgPayloadSize),
	}
	packet := &p2p.Packet{
		Sum: &p2p.Packet_PacketMsg{PacketMsg: msg},
	}
	_, err := protoWriter.WriteMsg(packet)
	require.NoError(t, err)
	chID, gotData, err := mconnServer.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, ChannelID(0x01), chID)
	require.True(t, bytes.Equal(gotData, msg.Data))

	// send msg thats too long
	msg.Data = make([]byte, mconnClient.config.MaxPacketMsgPayloadSize+100)

	// Depending on when the server will terminate the connection,
	// writing may fail or succeed.
	_, _ = protoWriter.WriteMsg(packet)
	got, err := mconnServer.handle.Join(ctx)
	require.NoError(t, err)
	if want := (errBadEncoding{}); !errors.As(got, &want) {
		t.Fatalf("expected errBadEncoding, got %v", err)
	}
}

func TestMConnectionReadErrorUnknownMsgType(t *testing.T) {
	ctx := t.Context()
	mconnClient, mconnServer := newConns(t)

	// send msg with unknown msg type
	_, err := protoio.NewDelimitedWriter(mconnClient.conn).WriteMsg(&types.Header{ChainID: "x"})
	require.NoError(t, err)
	got, err := mconnServer.handle.Join(ctx)
	require.NoError(t, err)
	if want := (errBadEncoding{}); !errors.As(got, &want) {
		t.Fatalf("got %v, want %T", got, want)
	}
}

func TestConnVectors(t *testing.T) {
	testCases := []struct {
		testName string
		packet   *p2p.Packet
		expBytes string
	}{
		{"PacketPing", pingMsg(), "0a00"},
		{"PacketPong", pongMsg(), "1200"},
		{"PacketMsg", &p2p.Packet{Sum: &p2p.Packet_PacketMsg{
			PacketMsg: &p2p.PacketMsg{
				ChannelID: 1, EOF: false, Data: []byte("data transmitted over the wire"),
			},
		}}, "1a2208011a1e64617461207472616e736d6974746564206f766572207468652077697265"},
	}

	for _, tc := range testCases {
		bz, err := tc.packet.Marshal()
		require.NoError(t, err, tc.testName)
		require.Equal(t, tc.expBytes, hex.EncodeToString(bz), tc.testName)
	}
}

func TestMConnectionChannelOverflow(t *testing.T) {
	ctx := t.Context()
	m1, m2 := newConns(t)
	protoWriter := protoio.NewDelimitedWriter(m1.conn)

	msg := &p2p.PacketMsg{
		ChannelID: 0x01,
		EOF:       true,
		Data:      []byte(`42`),
	}
	packet := &p2p.Packet{
		Sum: &p2p.Packet_PacketMsg{PacketMsg: msg},
	}
	_, err := protoWriter.WriteMsg(packet)
	require.NoError(t, err)
	_, _, err = m2.Recv(ctx)
	require.NoError(t, err)
	msg.ChannelID = int32(1025)
	_, err = protoWriter.WriteMsg(packet)
	require.NoError(t, err)
	got, err := m2.handle.Join(ctx)
	require.NoError(t, err)
	var want errBadChannel
	if !errors.As(got, &want) {
		t.Fatalf("got %v, want %T", got, want)
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

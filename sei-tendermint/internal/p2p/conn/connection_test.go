package conn

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/tendermint/tendermint/internal/libs/protoio"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"github.com/tendermint/tendermint/proto/tendermint/p2p"
)

const maxPingPongPacketSize = 1024 // bytes

func newMConnection(conn net.Conn) *MConnection {
	chDescs := []*ChannelDescriptor{{ID: 0x01, Priority: 1, SendQueueCapacity: 1}}
	return newMConnectionWithCh(conn, chDescs)

}

func newMConnectionWithCh(
	conn net.Conn,
	chDescs []*ChannelDescriptor,
) *MConnection {
	cfg := DefaultMConnConfig()
	cfg.PingInterval = 250 * time.Millisecond
	cfg.PongTimeout = 500 * time.Millisecond
	return NewMConnection(log.NewNopLogger(), conn, chDescs, cfg)
}

func mayDisconnectAfterDone(ctx context.Context, err error) error {
	err = utils.IgnoreCancel(err)
	if err == nil || ctx.Err() == nil || !IsDisconnect(err) {
		return err
	}
	return nil
}

func TestMConnectionSendRecv(t *testing.T) {
	t.Cleanup(leaktest.CheckTimeout(t, 10*time.Second))
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		server, client := tcp.TestPipe()
		mconn1 := newMConnection(client)
		s.SpawnBgNamed("mconn1", func() error { return mayDisconnectAfterDone(ctx, mconn1.Run(ctx)) })
		mconn2 := newMConnection(server)
		s.SpawnBgNamed("mconn2", func() error { return mayDisconnectAfterDone(ctx, mconn2.Run(ctx)) })

		rng := utils.TestRng()
		want := utils.GenBytes(rng, 20)
		if err := mconn2.Send(ctx, 0x01, want); err != nil {
			return fmt.Errorf("mconn2.Send(): %v", err)
		}
		_, got, err := mconn1.Recv(ctx)
		if err != nil {
			return fmt.Errorf("mconn1.Recv(): %v", err)
		}
		if err := utils.TestDiff(want, got); err != nil {
			return fmt.Errorf("mconn1.Recv(): %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
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
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		server, client := tcp.TestPipe()
		mconn := newMConnection(client)
		s.Spawn(func() error { return mconn.Run(ctx) })
		protoReader := protoio.NewDelimitedReader(server, maxPingPongPacketSize)
		protoWriter := protoio.NewDelimitedWriter(server)
		for range 3 {
			// read ping
			var got p2p.Packet
			if _, err := protoReader.ReadMsg(&got); err != nil {
				return fmt.Errorf("protoReader.ReadMsg(): %w", err)
			}
			if _, ok := got.Sum.(*p2p.Packet_PacketPing); !ok {
				return fmt.Errorf("expected ping, got %T", got.Sum)
			}

			// respond with pong
			if _, err := protoWriter.WriteMsg(pongMsg()); err != nil {
				return fmt.Errorf("protoWriter.WriteMsg(): %w", err)
			}
		}
		// Read until connection dies.
		_, _ = io.ReadAll(server)
		return nil
	})
	if !errors.Is(err, errPongTimeout) {
		t.Fatalf("expected pong timeout error, got %v", err)
	}
}

func TestMConnectionReadErrorBadEncoding(t *testing.T) {
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		c1, c2 := tcp.TestPipe()
		s.Spawn(func() error {
			mconn := newMConnection(c1)
			if got, want := mconn.Run(ctx), (errBadEncoding{}); !errors.As(got, &want) {
				return fmt.Errorf("got %v, want %T", got, want)
			}
			return nil
		})

		defer c2.Close()
		if _, err := c2.Write([]byte{1, 2, 3, 4, 5}); err != nil {
			return fmt.Errorf("c2.Write(): %w", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMConnectionReadErrorUnknownChannel(t *testing.T) {
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		server, client := tcp.TestPipe()
		// create client conn with two channels
		chDescs := []*ChannelDescriptor{
			{ID: 0x01, Priority: 1, SendQueueCapacity: 1},
			{ID: 0x02, Priority: 1, SendQueueCapacity: 1},
		}
		mconnClient := newMConnectionWithCh(client, chDescs)
		mconnServer := newMConnection(server)
		s.Spawn(func() error {
			if err := mconnClient.Run(ctx); !IsDisconnect(err) {
				return fmt.Errorf("got %v, want disconnect error", err)
			}
			return nil
		})
		s.Spawn(func() error {
			if err, want := mconnServer.Run(ctx), (errBadChannel{}); !errors.As(err, &want) {
				return fmt.Errorf("got %v, want %T", err, want)
			}
			return nil
		})
		msg := []byte("Ant-Man")

		// fail to send msg on channel unknown by client
		if got, want := mconnClient.Send(ctx, 0x04, msg), (errBadChannel{}); !errors.As(got, &want) {
			return fmt.Errorf("got %v, want %T", got, want)
		}
		// send msg on channel unknown by the server.
		// should cause an error on the server side.
		if err := mconnClient.Send(ctx, 0x02, msg); err != nil {
			return fmt.Errorf("mconnClient.Send(): %w", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMConnectionReadErrorLongMessage(t *testing.T) {
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		c1, c2 := tcp.TestPipe()
		mconn1 := newMConnection(c1)
		s.Spawn(func() error {
			if err, want := mconn1.Run(ctx), (errBadEncoding{}); !errors.As(err, &want) {
				return fmt.Errorf("expected errBadEncoding, got %v", err)
			}
			return nil
		})

		defer c2.Close()
		protoWriter := protoio.NewDelimitedWriter(c2)

		// send msg thats just right
		msg := &p2p.PacketMsg{
			ChannelID: 0x01,
			EOF:       true,
			Data:      make([]byte, mconn1.config.MaxPacketMsgPayloadSize),
		}
		packet := &p2p.Packet{
			Sum: &p2p.Packet_PacketMsg{PacketMsg: msg},
		}
		if _, err := protoWriter.WriteMsg(packet); err != nil {
			return fmt.Errorf("protoWriter.WriteMsg(): %w", err)
		}
		chID, gotData, err := mconn1.Recv(ctx)
		if err != nil {
			return fmt.Errorf("mconn.Recv(): %w", err)
		}
		if chID != ChannelID(0x01) {
			return fmt.Errorf("got channel ID %v, want 1", chID)
		}
		if !bytes.Equal(gotData, msg.Data) {
			return fmt.Errorf("mconn.Recv(): data mismatch")
		}

		// send msg thats too long
		msg.Data = make([]byte, mconn1.config.MaxPacketMsgPayloadSize+100)

		// Depending on when the server will terminate the connection,
		// writing may fail or succeed.
		_, _ = protoWriter.WriteMsg(packet)
		return nil
	})
	if err != nil {
		t.Fatal(err)
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
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		c1, c2 := tcp.TestPipe()
		m1 := newMConnection(c1)
		s.Spawn(func() error {
			if err, want := m1.Run(ctx), (&errBadChannel{}); !errors.As(err, want) {
				return fmt.Errorf("got %v, want %T", err, want)
			}
			return nil
		})
		defer c2.Close()
		protoWriter := protoio.NewDelimitedWriter(c2)

		msg := &p2p.PacketMsg{
			ChannelID: int32(1025),
			EOF:       true,
			Data:      []byte(`42`),
		}
		packet := &p2p.Packet{
			Sum: &p2p.Packet_PacketMsg{PacketMsg: msg},
		}
		if _, err := protoWriter.WriteMsg(packet); err != nil {
			return fmt.Errorf("protoWriter.WriteMsg(): %w", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

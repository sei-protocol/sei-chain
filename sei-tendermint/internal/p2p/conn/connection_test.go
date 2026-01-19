package conn

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/fortytw2/leaktest"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"github.com/tendermint/tendermint/proto/tendermint/p2p"
)

var ephKey = genEphKey()

// Wraps conn into encrypted conn, which is buffered.
// With hardcoded keys it doesn't provide any security, but that's just for test.
func withEnc(conn Conn) Conn {
	return newSecretConnection(conn,ephKey,ephKey.public)
}

const maxPingPongPacketSize = 1024 // bytes

func newMConnection(conn Conn) *MConnection {
	chDescs := []*ChannelDescriptor{{ID: 0x01, Priority: 1, SendQueueCapacity: 1}}
	return newMConnectionWithCh(conn, chDescs)
}

func newMConnectionWithCh(
	conn Conn,
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
		s.SpawnBg(func() error { return server.Run(ctx) })
		s.SpawnBg(func() error { return client.Run(ctx) })
		mconn1 := newMConnection(withEnc(client))
		s.SpawnBgNamed("mconn1", func() error { return mayDisconnectAfterDone(ctx, mconn1.Run(ctx)) })
		mconn2 := newMConnection(withEnc(server))
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
		s.SpawnBg(func() error { return server.Run(ctx) })
		s.SpawnBg(func() error { return client.Run(ctx) })
		mconn := newMConnection(withEnc(client))
		s.Spawn(func() error { return mconn.Run(ctx) })
		for range 3 {
			// read ping
			var got p2p.Packet
			gotBytes,err := ReadSizedMsg(ctx,server,maxPingPongPacketSize)
			if err!=nil { return fmt.Errorf("ReadSizedMsg(): %w",err) }
			if err:=gogoproto.Unmarshal(gotBytes,&got); err!=nil { return err }
			if _, ok := got.Sum.(*p2p.Packet_PacketPing); !ok {
				return fmt.Errorf("expected ping, got %T", got.Sum)
			}

			// respond with pong
			if err := WriteSizedMsg(ctx,server,utils.OrPanic1(gogoproto.Marshal(pongMsg()))); err != nil {
				return fmt.Errorf("WriteSizedMsg(): %w", err)
			}
		}
		// Read until connection dies.
		var buf [1024]byte
		for {
			if err:=server.Read(ctx,buf[:]); err!=nil { return nil }
		}
	})
	if !errors.Is(err, errPongTimeout) {
		t.Fatalf("expected pong timeout error, got %v", err)
	}
}

func TestMConnectionReadErrorBadEncoding(t *testing.T) {
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		c1, c2 := tcp.TestPipe()
		s.SpawnBg(func() error { return c1.Run(ctx) })
		s.SpawnBg(func() error { return c2.Run(ctx) })
		m1 := newMConnection(withEnc(c1))
		m2 := withEnc(c2)
		s.Spawn(func() error {
			if got, want := m1.Run(ctx), (errBadEncoding{}); !errors.As(got, &want) {
				return fmt.Errorf("got %v, want %T", got, want)
			}
			return nil
		})

		if err := m2.Write(ctx,[]byte{1, 2, 3, 4, 5}); err != nil {
			return fmt.Errorf("m2.Write(): %w", err)
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
		s.SpawnBg(func() error { return server.Run(ctx) })
		s.SpawnBg(func() error { return client.Run(ctx) })
		// create client conn with two channels
		chDescs := []*ChannelDescriptor{
			{ID: 0x01, Priority: 1, SendQueueCapacity: 1},
			{ID: 0x02, Priority: 1, SendQueueCapacity: 1},
		}
		mconnClient := newMConnectionWithCh(withEnc(client), chDescs)
		mconnServer := newMConnection(withEnc(server))
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
		s.SpawnBg(func() error { return c1.Run(ctx) })
		s.SpawnBg(func() error { return c2.Run(ctx) })
		mconn1 := newMConnection(withEnc(c1))
		mconn2 := withEnc(c2)
		s.Spawn(func() error {
			if err, want := mconn1.Run(ctx), (errBadEncoding{}); !errors.As(err, &want) {
				return fmt.Errorf("expected errBadEncoding, got %v", err)
			}
			return nil
		})

		// send msg thats just right
		msg := &p2p.PacketMsg{
			ChannelId: 0x01,
			Eof:       true,
			Data:      make([]byte, mconn1.config.MaxPacketMsgPayloadSize),
		}
		packet := utils.OrPanic1(gogoproto.Marshal(&p2p.Packet{
			Sum: &p2p.Packet_PacketMsg{PacketMsg: msg},
		}))
		if err := WriteSizedMsg(ctx,mconn2,packet); err != nil {
			return fmt.Errorf("WriteSizedMsg(): %w", err)
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
		_ = WriteSizedMsg(ctx,mconn2,packet)
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
				ChannelId: 1, Eof: false, Data: []byte("data transmitted over the wire"),
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
		s.SpawnBg(func() error { return c1.Run(ctx) })
		s.SpawnBg(func() error { return c2.Run(ctx) })
		m1 := newMConnection(withEnc(c1))
		m2 := withEnc(c2)
		s.Spawn(func() error {
			if err, want := m1.Run(ctx), (&errBadChannel{}); !errors.As(err, want) {
				return fmt.Errorf("got %v, want %T", err, want)
			}
			return nil
		})
		msg := &p2p.PacketMsg{
			ChannelId: int32(1025),
			Eof:       true,
			Data:      []byte(`42`),
		}
		packet := utils.OrPanic1(gogoproto.Marshal(&p2p.Packet{
			Sum: &p2p.Packet_PacketMsg{PacketMsg: msg},
		}))
		if err := WriteSizedMsg(ctx,m2,packet); err != nil {
			return fmt.Errorf("WriteSizedMsg(): %w", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

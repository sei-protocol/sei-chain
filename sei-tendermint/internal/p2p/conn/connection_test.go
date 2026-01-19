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

func spawnBgPipe(ctx context.Context, s scope.Scope) (Conn,Conn) {
	c1,c2 := tcp.TestPipe()
	s.SpawnBg(func() error {
		_ = c1.Run(ctx)
		return nil
	})
	s.SpawnBg(func() error {
		_ = c2.Run(ctx)
		return nil
	})
	return withEnc(c1,c2)
}

func withEnc(c1,c2 Conn) (Conn,Conn) {
	k1 := genEphKey()
	k2 := genEphKey()
	sc1 := newSecretConnection(c1,k1,k2.public)
	sc2 := newSecretConnection(c2,k2,k1.public)
	return sc1,sc2
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

func TestMConnectionSendRecv(t *testing.T) {
	t.Cleanup(leaktest.CheckTimeout(t, 10*time.Second))
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		client, server := spawnBgPipe(ctx,s)
		mconn1 := newMConnection(client)
		s.SpawnBgNamed("mconn1", func() error { return utils.IgnoreCancel(mconn1.Run(ctx)) })
		mconn2 := newMConnection(server)
		s.SpawnBgNamed("mconn2", func() error { return utils.IgnoreCancel(mconn2.Run(ctx)) })

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
		c1, c2 := tcp.TestPipe()
		s.SpawnBg(func() error { return c1.Run(ctx) })
		s.SpawnBg(func() error {
			_ = c2.Run(ctx)
			return nil
		})
		client,server := withEnc(c1,c2)
		s.Spawn(func() error { return newMConnection(client).Run(ctx) })
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
		c1, c2 := spawnBgPipe(ctx,s) 
		m1 := newMConnection(c1)
		s.Spawn(func() error {
			if err := m1.Run(ctx); !utils.ErrorAs[errBadEncoding](err).IsPresent() {
				return fmt.Errorf("got %v, want %T", err, errBadEncoding{})
			}
			return nil
		})

		if err := c2.Write(ctx,[]byte{1, 2, 3, 4, 5}); err != nil {
			return fmt.Errorf("c2.Write(): %w", err)
		}
		return c2.Flush(ctx)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMConnectionReadErrorUnknownChannel(t *testing.T) {
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {	
		// create client conn with two channels
		chDescs := []*ChannelDescriptor{
			{ID: 0x01, Priority: 1, SendQueueCapacity: 1},
			{ID: 0x02, Priority: 1, SendQueueCapacity: 1},
		}
		client,server := spawnBgPipe(ctx,s)
		mconnClient := newMConnectionWithCh(client, chDescs)
		mconnServer := newMConnection(server)
		
		s.SpawnBg(func() error { return utils.IgnoreCancel(mconnClient.Run(ctx)) })
		s.Spawn(func() error {
			if err, want := mconnServer.Run(ctx), (errBadChannel{}); !errors.As(err, &want) {
				return fmt.Errorf("got %v, want %T", err, want)
			}
			return nil
		})
		msg := []byte("Ant-Man")

		// fail to send msg on channel unknown by client
		if got := mconnClient.Send(ctx, 0x04, msg); !utils.ErrorAs[errBadChannel](got).IsPresent() {
			return fmt.Errorf("got %v, want errBadChannel", got)
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
		c1,c2 := spawnBgPipe(ctx,s)
		mconn1 := newMConnection(c1)
		s.Spawn(func() error {
			if err := mconn1.Run(ctx); !errors.Is(err,errMsgTooLarge) {
				return fmt.Errorf("expected %v, got %v", errMsgTooLarge, err)
			}
			return nil
		})

		t.Log("send msg thats just right")
		msg := &p2p.PacketMsg{
			ChannelId: 0x01,
			Eof:       true,
			Data:      make([]byte, mconn1.config.MaxPacketMsgPayloadSize),
		}
		packet := utils.OrPanic1(gogoproto.Marshal(&p2p.Packet{
			Sum: &p2p.Packet_PacketMsg{PacketMsg: msg},
		}))
		if err := WriteSizedMsg(ctx,c2,packet); err != nil {
			return fmt.Errorf("WriteSizedMsg(): %w", err)
		}
		if err := c2.Flush(ctx); err!=nil {
			return fmt.Errorf("c2.Flush(): %w",err)
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

		t.Log("send msg thats too long")
		msg.Data = make([]byte, mconn1.config.MaxPacketMsgPayloadSize+100)
		packet = utils.OrPanic1(gogoproto.Marshal(&p2p.Packet{
			Sum: &p2p.Packet_PacketMsg{PacketMsg: msg},
		}))
		// Depending on when the server will terminate the connection,
		// writing may fail or succeed.
		_ = WriteSizedMsg(ctx,c2,packet)
		_ = c2.Flush(ctx)
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
		c1, c2 := spawnBgPipe(ctx,s)
		m1 := newMConnection(c1)
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
		if err := WriteSizedMsg(ctx,c2,packet); err != nil {
			return fmt.Errorf("WriteSizedMsg(): %w", err)
		}
		return c2.Flush(ctx)
	})
	if err != nil {
		t.Fatal(err)
	}
}

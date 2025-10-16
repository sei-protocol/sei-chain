package p2p

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gogo/protobuf/proto"
	gogotypes "github.com/gogo/protobuf/types"
	dbm "github.com/tendermint/tm-db"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"github.com/tendermint/tendermint/types"
)

func mayDisconnectAfterDone(ctx context.Context, err error) error {
	err = utils.IgnoreCancel(err)
	if err == nil || ctx.Err() == nil || !conn.IsDisconnect(err) {
		return err
	}
	return nil
}

func echoReactor(ctx context.Context, channel *Channel) {
	for {
		m,err := channel.Recv(ctx)
		if err!=nil { return }
		channel.Send(m.Message, m.From)
	}
}

func TestRouter_Network(t *testing.T) {
	ctx := t.Context()

	t.Cleanup(leaktest.Check(t))

	t.Logf("Create a test network and open a channel where all peers run echoReactor.")
	network := MakeTestNetwork(t, TestNetworkOptions{NumNodes: 8})
	local := network.RandomNode()
	peers := network.Peers(local.NodeID)
	chDesc := makeChDesc(5)
	channels := network.MakeChannels(t, chDesc)

	network.Start(t)

	channel := channels[local.NodeID]
	for _, peer := range peers {
		go echoReactor(ctx, channels[peer.NodeID])
	}

	t.Logf("Sending a message to each peer should work.")
	for _, peer := range peers {
		msg := &TestMessage{Value: "foo"}
		channel.Send(msg, peer.NodeID)
		RequireReceive(t, channel, RecvMsg{From: peer.NodeID, Message: msg})
	}

	t.Logf("Sending a broadcast should return back a message from all peers.")
	channel.Broadcast(&TestMessage{Value: "bar"})
	want := []RecvMsg{}
	for _, peer := range peers {
		want = append(want, RecvMsg{
			From:      peer.NodeID,
			Message:   &TestMessage{Value: "bar"},
		})
	}
	RequireReceiveUnordered(t, channel, want)

	t.Logf("We then submit an error for a peer, and watch it get disconnected and")
	t.Logf("then reconnected as the router retries it.")
	peerUpdates := local.PeerManager.Subscribe(ctx)
	channel.SendError(PeerError{
		NodeID: peers[0].NodeID,
		Err:    errors.New("boom"),
	})
	RequireUpdates(t, peerUpdates, []PeerUpdate{
		{NodeID: peers[0].NodeID, Status: PeerStatusDown},
		{NodeID: peers[0].NodeID, Status: PeerStatusUp},
	})
}

func TestRouter_Channel_Basic(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	logger, _ := log.NewDefaultLogger("plain", "debug")
	ctx := t.Context()
	chDesc := makeChDesc(5)

	// Set up a router with no transports (so no peers).
	peerManager, err := NewPeerManager(logger, selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
	require.NoError(t, err)

	router, err := NewRouter(
		logger,
		NopMetrics(),
		selfKey,
		peerManager,
		func() *types.NodeInfo { return &selfInfo },
		nil,
		RouterOptions{
			Endpoint:   Endpoint{tcp.TestReserveAddr()},
			Connection: conn.DefaultMConnConfig(),
		},
	)
	require.NoError(t, err)

	require.NoError(t, router.Start(ctx))
	t.Cleanup(router.Wait)

	t.Logf("Opening a channel should work.")
	channel, err := router.OpenChannel(chDesc)
	require.NoError(t, err)
	require.NotNil(t, channel)

	t.Logf("Opening the same channel again should fail.")
	_, err = router.OpenChannel(chDesc)
	require.Error(t, err)

	t.Logf("Opening a different channel should work.")
	chDesc2 := ChannelDescriptor{ID: 2, MessageType: &TestMessage{}}
	_, err = router.OpenChannel(chDesc2)
	require.NoError(t, err)

	t.Logf("We should be able to send on the channel, even though there are no peers.")
	channel.Send(&TestMessage{Value: "foo"}, types.NodeID(strings.Repeat("a", 40)))

	t.Logf("A message to ourselves should be dropped.")
	channel.Send(&TestMessage{Value: "self"}, selfID)
	RequireEmpty(t, channel)
}

func TestRouter_SendReceive(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	chDesc := makeChDesc(5)

	t.Logf("Create a test network and open a channel on all nodes.")
	network := MakeTestNetwork(t, TestNetworkOptions{NumNodes: 3})

	ids := network.NodeIDs()
	aID, bID, cID := ids[0], ids[1], ids[2]
	channels := network.MakeChannels(t, chDesc)
	a, b, c := channels[aID], channels[bID], channels[cID]
	otherChannels := network.MakeChannels(t, MakeTestChannelDesc(9))

	network.Start(t)

	t.Logf("Sending a message a->b should work, and not send anything further to a, b, or c.")
	a.Send(&TestMessage{Value: "foo"}, bID)
	RequireReceive(t, b, RecvMsg{From: aID, Message: &TestMessage{Value: "foo"}})
	RequireEmpty(t, a, b, c)

	t.Logf("Sending a different message type should be dropped.")
	a.Send(&gogotypes.BoolValue{Value: true}, bID)
	RequireEmpty(t, a, b, c)

	t.Logf("Sending to an unknown peer should be dropped.")
	a.Send(&TestMessage{Value: "a"}, types.NodeID(strings.Repeat("a", 40)))
	RequireEmpty(t, a, b, c)

	t.Logf("Sending to self should be dropped.")
	a.Send(&TestMessage{Value: "self"}, aID)
	RequireEmpty(t, a, b, c)

	t.Logf("Removing b and sending to it should be dropped.")
	network.Remove(t, bID)
	a.Send(&TestMessage{Value: "nob"}, bID)
	RequireEmpty(t, a, b, c)

	t.Logf("After all this, sending a message c->a should work.")
	c.Send(&TestMessage{Value: "bar"}, aID)
	RequireReceive(t, a, RecvMsg{From: cID, Message: &TestMessage{Value: "bar"}})
	RequireEmpty(t, a, b, c)

	t.Logf("None of these messages should have made it onto the other channels.")
	for _, other := range otherChannels {
		RequireEmpty(t, other)
	}
}

func TestRouter_Channel_Broadcast(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	chDesc := makeChDesc(5)

	t.Logf("Create a test network and open a channel on all nodes.")
	network := MakeTestNetwork(t, TestNetworkOptions{NumNodes: 4})

	ids := network.NodeIDs()
	aID, bID, cID, dID := ids[0], ids[1], ids[2], ids[3]
	channels := network.MakeChannels(t, chDesc)
	a, b, c, d := channels[aID], channels[bID], channels[cID], channels[dID]

	network.Start(t)

	t.Logf("Sending a broadcast from b should work.")
	b.Broadcast(&TestMessage{Value: "foo"})
	for _, ch := range utils.Slice(a,c,d) {
		RequireReceive(t, ch, RecvMsg{From: bID, Message: &TestMessage{Value: "foo"}})
	}
	RequireEmpty(t, a, b, c, d)

	t.Logf("Removing one node from the network shouldn't prevent broadcasts from working.")
	network.Remove(t, dID)
	b.Broadcast(&TestMessage{Value: "bar"})
	for _, ch := range utils.Slice(b,c) {
		RequireReceive(t, ch, RecvMsg{From: aID, Message: &TestMessage{Value: "bar"}})
	}
	RequireEmpty(t, a, b, c, d)
}

func TestRouter_Channel_Wrapper(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	t.Logf("Create a test network and open a channel on all nodes.")
	network := MakeTestNetwork(t, TestNetworkOptions{NumNodes: 2})

	ids := network.NodeIDs()
	aID, bID := ids[0], ids[1]
	chDesc := ChannelDescriptor{
		ID:                  17,
		MessageType:         &wrapperMessage{},
		Priority:            5,
		SendQueueCapacity:   10,
		RecvBufferCapacity:  10,
		RecvMessageCapacity: 10,
	}

	channels := network.MakeChannels(t, chDesc)
	a, b := channels[aID], channels[bID]

	network.Start(t)

	// Since wrapperMessage implements Wrapper and handles Message, it
	// should automatically wrap and unwrap sent messages -- we prepend the
	// wrapper actions to the message value to signal this.
	a.Send(&TestMessage{Value: "foo"},bID)
	RequireReceive(t, b, RecvMsg{From: aID, Message: &TestMessage{Value: "unwrap:wrap:foo"}})

	// If we send a different message that can't be wrapped, it should be dropped.
	a.Send(&gogotypes.BoolValue{Value: true},bID)
	RequireEmpty(t, b)

	// If we send the wrapper message itself, it should also be passed through
	// since WrapperMessage supports it, and should only be unwrapped at the receiver.
	a.Send(&wrapperMessage{TestMessage: TestMessage{Value: "foo"}}, bID)
	RequireReceive(t, b, RecvMsg{
		From:      aID,
		Message:   &TestMessage{Value: "unwrap:foo"},
	})
}

// WrapperMessage prepends the value with "wrap:" and "unwrap:" to test it.
type wrapperMessage struct {
	TestMessage
}

var _ Wrapper = (*wrapperMessage)(nil)

func (w *wrapperMessage) Wrap(inner proto.Message) error {
	switch inner := inner.(type) {
	case *TestMessage:
		w.TestMessage.Value = fmt.Sprintf("wrap:%v", inner.Value)
	case *wrapperMessage:
		*w = *inner
	default:
		return fmt.Errorf("invalid message type %T", inner)
	}
	return nil
}

func (w *wrapperMessage) Unwrap() (proto.Message, error) {
	return &TestMessage{Value: fmt.Sprintf("unwrap:%v", w.Value)}, nil
}

func TestRouter_Channel_Error(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	chDesc := makeChDesc(5)
	ctx := t.Context()

	t.Logf("Create a test network and open a channel on all nodes.")
	network := MakeTestNetwork(t, TestNetworkOptions{NumNodes: 3})
	network.Start(t)

	ids := network.NodeIDs()
	aID, bID := ids[0], ids[1]
	channels := network.MakeChannels(t, chDesc)
	a := channels[aID]

	t.Logf("Erroring b should cause it to be disconnected. It will reconnect shortly after.")
	sub := network.Node(aID).MakePeerUpdates(ctx, t)
	a.SendError(PeerError{NodeID: bID, Err: errors.New("boom")})
	RequireUpdates(t, sub, []PeerUpdate{
		{NodeID: bID, Status: PeerStatusDown},
		{NodeID: bID, Status: PeerStatusUp},
	})
}

var keyFiltered, infoFiltered = makeKeyAndInfo()

func makeRouterWithOptions(logger log.Logger, ropts RouterOptions) *Router {
	// Set up and start the router.
	opts := PeerManagerOptions{
		MinRetryTime: 100 * time.Millisecond,
	}
	key,info := makeKeyAndInfo()
	peerManager, err := NewPeerManager(logger, info.NodeID, dbm.NewMemDB(), opts, NopMetrics())
	if err!=nil {
		panic(fmt.Errorf("NewPeerManager: %w", err))
	}
	router, err := NewRouter(
		logger.With("node", info.NodeID),
		NopMetrics(),
		key,
		peerManager,
		func() *types.NodeInfo { return &info },
		func(_ context.Context, id types.NodeID) error {
			if id == infoFiltered.NodeID {
				return errors.New("should filter")
			}
			return nil
		},
		ropts,
	)
	if err!=nil {
		panic(fmt.Errorf("NewRouter: %w", err))
	}
	return router
}

func makeRouterOptions() RouterOptions {
	return RouterOptions{
		DialSleep:          func(context.Context) error { return nil },
		NumConcurrentDials: func() int { return 100 },
		Endpoint:           Endpoint{tcp.TestReserveAddr()},
		Connection:         conn.DefaultMConnConfig(),
	}
}

func makeRouter(logger log.Logger) *Router {
	return makeRouterWithOptions(logger, makeRouterOptions())
}

func TestRouter_FilterByIP(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	ctx := t.Context()
	t.Cleanup(leaktest.Check(t))

	var reject atomic.Bool
	opts := makeRouterOptions()
	opts.FilterPeerByIP = func(ctx context.Context, addr netip.AddrPort) error {
		if reject.Load() {
			return errors.New("fail all")
		}
		return nil
	}
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		r := makeRouterWithOptions(logger, opts)
		s.SpawnBg(func() error { return r.Run(ctx) })
		sub := r.peerManager.Subscribe(ctx)

		t.Logf("Connection should succeed.")
		r2 := makeRouter(logger)
		tcpConn,err := r2.Dial(ctx, r.Address())
		if err != nil {
			return fmt.Errorf("peerTransport.Dial(): %w", err)
		}
		defer tcpConn.Close()
		conn, err := HandshakeOrClose(ctx, r2, tcpConn)
		if err != nil {
			return fmt.Errorf("conn.Handshake(): %w", err)
		}
		RequireUpdate(t, sub, PeerUpdate{
			NodeID: r2.Address().NodeID,
			Status: PeerStatusUp,
		})
		conn.Close()

		t.Logf("Enable filtering.")
		reject.Store(true)

		t.Logf("Connection should fail during handshake.")
		r2 = makeRouter(logger)
		tcpConn, err = r2.Dial(ctx, r.Address())
		if err != nil {
			return fmt.Errorf("peerTransport.Dial(): %w", err)
		}
		defer tcpConn.Close()
		if _, err := HandshakeOrClose(ctx, r2, tcpConn); err == nil {
			return fmt.Errorf("handshake(): expected error")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRouter_AcceptPeers(t *testing.T) {
	key, info := makeKeyAndInfo()
	info2 := info
	info2.Network = "other-network"
	info3 := info
	info3.Channels = []byte{0x23}
	testcases := map[string]struct {
		info types.NodeInfo
		key  crypto.PrivKey
		ok   bool
	}{
		"valid handshake":       {info, key, true},
		"empty handshake":       {types.NodeInfo{}, key, false},
		"self handshake":        {selfInfo, selfKey, false},
		"incompatible network":  {info2, key, false},
		"incompatible channels": {info3, key, false},
		"filtered":              {infoFiltered, keyFiltered, false},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			logger, _ := log.NewDefaultLogger("plain", "debug")
			t.Cleanup(leaktest.Check(t))
			if err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
				r := makeRouter(logger)
				s.SpawnBg(func() error { return r.Run(ctx) })
				sub := r.peerManager.Subscribe(ctx)

				r2 := makeRouter(logger)
				// WARNING: here we malform the router internal state.
				// It would be better to have a dedicated API for performing malformed handshakes.
				r2.privKey = tc.key
				*r2.nodeInfoProducer() = tc.info
				tcpConn,err := r2.Dial(ctx, r.Address())
				if err != nil {
					return fmt.Errorf("peerTransport.Dial(): %w", err)
				}
				defer tcpConn.Close()
				if tc.ok {
					if _, err := HandshakeOrClose(ctx, r2, tcpConn); err != nil {
						return fmt.Errorf("conn.Handshake(): %w", err)
					}
					RequireUpdate(t, sub, PeerUpdate{
						NodeID: tc.info.NodeID,
						Status: PeerStatusUp,
					})
				} else {
					// Expect immediate or delayed failure.
					// Peer should drop the connection during handshake.
					conn, err := HandshakeOrClose(ctx, r2, tcpConn)
					if err != nil {
						return nil
					}
					if err := conn.Run(ctx, r2); !errors.Is(err, io.EOF) {
						return fmt.Errorf("want EOF, got %w", err)
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Test checking that multiple peers connecting at once don't block each other.
func TestRouter_AcceptPeers_Parallel(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	ctx := t.Context()
	t.Cleanup(leaktest.Check(t))

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		t.Logf("Set up and start the router.")
		r := makeRouter(logger)
		s.SpawnBg(func() error { return r.Run(ctx) })
		sub := r.peerManager.Subscribe(ctx)

		t.Logf("Dial raw connections.")
		var peers []*Router
		var conns []*net.TCPConn
		for range 10 {
			x := makeRouter(logger)
			peers = append(peers, x)
			conn, err := x.Dial(ctx, r.Address())
			if err != nil {
				return fmt.Errorf("x.Dial(): %w", err)
			}
			defer conn.Close()
			conns = append(conns, conn)
		}
		t.Logf("Handshake the connections in reverse order.")
		for i := len(conns) - 1; i >= 0; i-- {
			if _, err := HandshakeOrClose(ctx, peers[i], conns[i]); err != nil {
				return fmt.Errorf("conn.Handshake(): %w", err)
			}
			RequireUpdate(t, sub, PeerUpdate{
				NodeID:	peers[i].Address().NodeID,
				Status: PeerStatusUp,
			})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRouter_DialPeer_Retry(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	t.Cleanup(leaktest.Check(t))

	if err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		t.Logf("Set up and start the router.")
		r := makeRouter(logger)
		s.SpawnBg(func() error { return r.Run(ctx) })
		sub := r.peerManager.Subscribe(ctx)

		r2 := makeRouter(logger)
		listener, err := tcp.Listen(r2.Endpoint().AddrPort)
		if err != nil {
			return fmt.Errorf("tcp.Listen(): %w", err)
		}
		defer listener.Close()

		t.Log("Populate peer manager.")
		if ok, err := r.peerManager.Add(r2.Address()); !ok || err != nil {
			return fmt.Errorf("peerManager.Add() = %v,%w", ok, err)
		}

		t.Log("Accept and drop.")
		conn, err := tcp.AcceptOrClose(ctx, listener)
		if err != nil {
			return fmt.Errorf("peerTransport.Dial(): %w", err)
		}
		conn.Close()

		t.Log("Accept and complete handshake.")
		conn, err = tcp.AcceptOrClose(ctx, listener)
		if err != nil {
			return fmt.Errorf("peerTransport.Dial(): %w", err)
		}
		defer conn.Close()
		if _, err := HandshakeOrClose(ctx, r2, conn); err != nil {
			return fmt.Errorf("conn.Handshake(): %w", err)
		}
		RequireUpdate(t, sub, PeerUpdate{
			NodeID: r2.Address().NodeID,
			Status: PeerStatusUp,
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

/*
func TestRouter_DialPeer_Reject(t *testing.T) {
	key, info := makeKeyAndInfo()
	_, info2 := makeKeyAndInfo()
	info3 := info
	info3.Network = "other-network"
	info4 := info
	info4.Channels = []byte{0x23}
	testcases := map[string]struct {
		dialID types.NodeID
		info   types.NodeInfo
	}{
		"empty handshake":       {info.NodeID, types.NodeInfo{}},
		"unexpected node ID":    {info2.NodeID, info},
		"incompatible network":  {info.NodeID, info3},
		"incompatible channels": {info.NodeID, info4},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			logger, _ := log.NewDefaultLogger("plain", "debug")
			t.Cleanup(leaktest.Check(t))
			ctx := t.Context()
			h := spawnRouter(t, logger)
			if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
				addr := tcp.TestReserveAddr()
				listener, err := tcp.Listen(addr)
				if err != nil {
					return fmt.Errorf("tcp.Listen(): %w", err)
				}
				defer listener.Close()
				if ok, err := h.peerManager.Add(Endpoint{addr}.NodeAddress(tc.dialID)); !ok || err != nil {
					return fmt.Errorf("peerManager.Add() = %v,%w", ok, err)
				}
				tcpConn, err := tcp.AcceptOrClose(ctx, listener)
				if err != nil {
					return fmt.Errorf("peerTransport.Accept(): %w", err)
				}
				defer tcpConn.Close()
				// Connections should be closed either during handshake, or immediately afterwards.
				conn, err := handshake(ctx, logger, tcpConn, tc.info, key)
				if err != nil {
					return nil
				}
				if err := conn.Run(ctx); !errors.Is(err, io.EOF) {
					return fmt.Errorf("want EOF, got %w", err)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRouter_DialPeers_Parallel(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	ctx := t.Context()
	t.Cleanup(leaktest.Check(t))

	var keys []crypto.PrivKey
	var infos []types.NodeInfo
	for range 10 {
		key, info := makeKeyAndInfo()
		keys = append(keys, key)
		infos = append(infos, info)
	}

	t.Logf("Set up and start the router.")
	h := spawnRouter(t, logger)
	sub := h.peerManager.Subscribe(ctx)

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		t.Logf("Accept raw connections.")
		var conns []*net.TCPConn
		for i, info := range infos {
			t.Logf("ACCEPT %v %v", i, info.NodeID)
			addr := tcp.TestReserveAddr()
			listener, err := tcp.Listen(addr)
			if err != nil {
				return fmt.Errorf("tcp.Listen(): %w", err)
			}
			defer listener.Close()
			if ok, err := h.peerManager.Add(Endpoint{addr}.NodeAddress(info.NodeID)); !ok || err != nil {
				return fmt.Errorf("peerManager.Add() = %v,%w", ok, err)
			}
			conn, err := tcp.AcceptOrClose(ctx, listener)
			if err != nil {
				return fmt.Errorf("peerTransport.Accept(): %w", err)
			}
			defer conn.Close()
			conns = append(conns, conn)
		}
		t.Logf("Handshake the connections in reverse order.")
		for i := len(conns) - 1; i >= 0; i-- {
			conn := conns[i]
			info := infos[i]
			if _, err := handshake(ctx, logger, conn, info, keys[i]); err != nil {
				return fmt.Errorf("conn.Handshake(): %w", err)
			}
			RequireUpdate(t, sub, PeerUpdate{
				NodeID: info.NodeID,
				Status: PeerStatusUp,
			})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRouter_EvictPeers(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	ctx := t.Context()
	t.Cleanup(leaktest.Check(t))
	h := spawnRouter(t, logger)
	sub := h.peerManager.Subscribe(ctx)

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		key, info := makeKeyAndInfo()
		tcpConn, err := tcp.Dial(ctx, h.router.Endpoint().AddrPort)
		if err != nil {
			return fmt.Errorf("Dial(): %w", err)
		}
		defer tcpConn.Close()
		conn, err := handshake(ctx, logger, tcpConn, info, key)
		if err != nil {
			return fmt.Errorf("conn.Handshake(): %w", err)
		}
		RequireUpdate(t, sub, PeerUpdate{
			NodeID: info.NodeID,
			Status: PeerStatusUp,
		})

		t.Log("Report the peer as bad.")
		h.peerManager.Errored(info.NodeID, errors.New("boom"))
		RequireUpdate(t, sub, PeerUpdate{
			NodeID: info.NodeID,
			Status: PeerStatusDown,
		})
		t.Log("Wait for conn down")
		if err := conn.Run(ctx); !errors.Is(err, io.EOF) {
			return fmt.Errorf("want EOF, got %w", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func makeChDesc(id ChannelID) ChannelDescriptor {
	return ChannelDescriptor{
		ID:                  id,
		MessageType:         &TestMessage{},
		Priority:            5,
		RecvBufferCapacity:  10,
		RecvMessageCapacity: 10000,
	}
}

func TestRouter_DontSendOnInvalidChannel(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	ctx := t.Context()
	t.Cleanup(leaktest.Check(t))
	h := spawnRouter(t, logger)
	sub := h.peerManager.Subscribe(ctx)

	desc1 := makeChDesc(1)
	ch1, err := h.router.OpenChannel(desc1)
	require.NoError(t, err)

	desc2 := makeChDesc(2)
	ch2, err := h.router.OpenChannel(desc2)
	require.NoError(t, err)

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		key, info := makeKeyAndInfo()
		info.Channels = []byte{byte(desc1.ID)}
		descs := []*ChannelDescriptor{desc1}
		tcpConn, err := tcp.Dial(ctx, h.router.Endpoint().AddrPort)
		if err != nil {
			return fmt.Errorf("Dial(): %w", err)
		}
		conn, err := HandshakeOrClose(
			ctx,
			logger.With("node", info.NodeID),
			info, key, tcpConn,
			conn.DefaultMConnConfig(),
			descs,
		)
		if err != nil {
			return fmt.Errorf("conn.Handshake(): %w", err)
		}
		RequireUpdate(t, sub, PeerUpdate{
			NodeID: info.NodeID,
			Status: PeerStatusUp,
		})
		s.SpawnBg(func() error { return mayDisconnectAfterDone(ctx, conn.Run(ctx)) })
		n := 1
		msg1 := &TestMessage{Value: "Hello"}
		msg2 := &TestMessage{Value: "Hello2"}
		t.Log("Broadcast messages of both channels.")
		s.Spawn(func() error {
			for range n {
				ch1.Broadcast(msg1)
				ch2.Broadcast(msg2)
			}
			return nil
		})
		t.Log("Expect messages of 1 channel only.")
		for range n {
			gotChID, gotMsg, err := conn.ReceiveMessage(ctx)
			if err != nil {
				return fmt.Errorf("ReceiveMessage(): %w", err)
			}
			if gotChID != desc1.ID {
				return fmt.Errorf("gotChID = %v, want %v", gotChID, desc1.ID)
			}
			got := proto.Clone(desc1.MessageType)
			if err := proto.Unmarshal(gotMsg, got); err != nil {
				return fmt.Errorf("Unmarshal: %w", err)
			}
			if err := utils.TestDiff[proto.Message](got, msg1); err != nil {
				return fmt.Errorf("gotMsg: %v", err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}*/

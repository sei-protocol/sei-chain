package p2p

import (
	"context"
	"errors"
	"fmt"
	"github.com/fortytw2/leaktest"
	"github.com/gogo/protobuf/proto"
	gogotypes "github.com/gogo/protobuf/types"
	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

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

func makeChDesc(id ChannelID) ChannelDescriptor {
	return ChannelDescriptor{
		ID:                  id,
		MessageType:         &TestMessage{},
		Priority:            5,
		RecvBufferCapacity:  10,
		RecvMessageCapacity: 10000,
	}
}

func echoReactor(ctx context.Context, channel *Channel) {
	for {
		m, err := channel.Recv(ctx)
		if err != nil {
			return
		}
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
			From:    peer.NodeID,
			Message: &TestMessage{Value: "bar"},
		})
	}
	RequireReceiveUnordered(t, channel, want)

	t.Logf("We report a fatal error and expect the peer to get disconnected")
	conn, ok := local.Router.peerManager.Conns().Get(peers[0].NodeID)
	require.True(t, ok)
	local.Router.Evict(peers[0].NodeID, errors.New("boom"))
	local.WaitForDisconnect(ctx, conn)
}

func TestRouter_Channel_Basic(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	logger, _ := log.NewDefaultLogger("plain", "debug")
	rng := utils.TestRng()
	ctx := t.Context()
	chDesc := makeChDesc(5)

	router := makeRouter(logger, rng)
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
	channel.Send(&TestMessage{Value: "self"}, TestAddress(router).NodeID)
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
	for _, ch := range utils.Slice(a, c, d) {
		RequireReceive(t, ch, RecvMsg{From: bID, Message: &TestMessage{Value: "foo"}})
	}
	RequireEmpty(t, a, b, c, d)

	t.Logf("Removing one node from the network shouldn't prevent broadcasts from working.")
	network.Remove(t, dID)
	a.Broadcast(&TestMessage{Value: "bar"})
	for _, ch := range utils.Slice(b, c) {
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
	a.Send(&TestMessage{Value: "foo"}, bID)
	RequireReceive(t, b, RecvMsg{From: aID, Message: &TestMessage{Value: "unwrap:wrap:foo"}})

	// If we send a different message that can't be wrapped, it should be dropped.
	a.Send(&gogotypes.BoolValue{Value: true}, bID)
	RequireEmpty(t, b)

	// If we send the wrapper message itself, it should also be passed through
	// since WrapperMessage supports it, and should only be unwrapped at the receiver.
	a.Send(&wrapperMessage{TestMessage: TestMessage{Value: "foo"}}, bID)
	RequireReceive(t, b, RecvMsg{
		From:    aID,
		Message: &TestMessage{Value: "unwrap:foo"},
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

func TestRouter_SendError(t *testing.T) {
	ctx := t.Context()
	t.Cleanup(leaktest.Check(t))
	t.Logf("Create a test network and open a channel on all nodes.")
	network := MakeTestNetwork(t, TestNetworkOptions{NumNodes: 2})
	network.Start(t)

	t.Logf("Erroring b should cause it to be disconnected.")
	nodes := network.Nodes()
	conn, ok := nodes[0].Router.peerManager.Conns().Get(nodes[1].NodeID)
	require.True(t, ok)
	nodes[0].Router.Evict(nodes[1].NodeID, errors.New("boom"))
	nodes[0].WaitForDisconnect(ctx, conn)
}

var keyFiltered = makeKey(utils.TestRngFromSeed(738234133))
var infoFiltered = makeInfo(keyFiltered)

func makeRouterWithOptionsAndKey(logger log.Logger, opts *RouterOptions, key crypto.PrivKey) *Router {
	info := makeInfo(key)
	r, err := NewRouter(
		logger.With("node", info.NodeID),
		NopMetrics(),
		key,
		func() *types.NodeInfo { return &info },
		dbm.NewMemDB(),
		opts,
	)
	if err != nil {
		panic(err)
	}
	return r
}

func makeRouterOptions() *RouterOptions {
	return &RouterOptions{
		MaxAcceptRate:      utils.Some(rate.Inf),
		MaxDialRate:        utils.Some(rate.Inf),
		MaxConcurrentDials: utils.Some(100),
		Endpoint:           Endpoint{tcp.TestReserveAddr()},
		Connection:         conn.DefaultMConnConfig(),
		FilterPeerByID: utils.Some(func(_ context.Context, id types.NodeID) error {
			if id == infoFiltered.NodeID {
				return errors.New("should filter")
			}
			return nil
		}),
	}
}

func makeRouterWithOptions(logger log.Logger, rng utils.Rng, opts *RouterOptions) *Router {
	return makeRouterWithOptionsAndKey(logger, opts, makeKey(rng))
}

func makeRouterWithKey(logger log.Logger, key crypto.PrivKey) *Router {
	return makeRouterWithOptionsAndKey(logger, makeRouterOptions(), key)
}

func makeRouter(logger log.Logger, rng utils.Rng) *Router {
	return makeRouterWithKey(logger, makeKey(rng))
}

func TestRouter_FilterByIP(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	ctx := t.Context()
	rng := utils.TestRng()
	t.Cleanup(leaktest.Check(t))

	var reject atomic.Bool
	opts := makeRouterOptions()
	opts.FilterPeerByIP = utils.Some(func(ctx context.Context, addr netip.AddrPort) error {
		if reject.Load() {
			return errors.New("fail all")
		}
		return nil
	})
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		r := makeRouterWithOptions(logger, rng, opts)
		s.SpawnBg(func() error { return utils.IgnoreCancel(r.Run(ctx)) })
		if err := r.WaitForStart(ctx); err != nil {
			return err
		}
		sub := r.peerManager.Subscribe()

		t.Logf("Connection should succeed.")
		r2 := makeRouter(logger, rng)
		addr := TestAddress(r)
		tcpConn, err := r2.dial(ctx, addr)
		if err != nil {
			return fmt.Errorf("peerTransport.dial(): %w", err)
		}
		defer tcpConn.Close()
		conn, err := r2.handshake(ctx, tcpConn, utils.Some(addr))
		if err != nil {
			return fmt.Errorf("handshake(): %w", err)
		}
		RequireUpdate(t, sub, PeerUpdate{
			NodeID: TestAddress(r2).NodeID,
			Status: PeerStatusUp,
		})
		conn.Close()

		t.Logf("Enable filtering.")
		reject.Store(true)

		t.Logf("Connection should fail during handshake.")
		r2 = makeRouter(logger, rng)
		tcpConn, err = r2.dial(ctx, addr)
		if err != nil {
			return fmt.Errorf("peerTransport.dial(): %w", err)
		}
		defer tcpConn.Close()
		if _, err := r2.handshake(ctx, tcpConn, utils.Some(addr)); err == nil {
			return fmt.Errorf("handshake(): expected error")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRouter_AcceptPeers(t *testing.T) {
	rng := utils.TestRng()
	selfKey := makeKey(rng)
	peerKey := makeKey(rng)
	badInfo := makeInfo(peerKey)
	badInfo.Network = "other-network"
	testcases := map[string]struct {
		info types.NodeInfo
		key  crypto.PrivKey
		ok   bool
	}{
		"valid handshake":      {makeInfo(peerKey), peerKey, true},
		"empty handshake":      {types.NodeInfo{}, peerKey, false},
		"self handshake":       {makeInfo(selfKey), selfKey, false},
		"incompatible network": {badInfo, peerKey, false},
		"filtered":             {infoFiltered, keyFiltered, false},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			logger, _ := log.NewDefaultLogger("plain", "debug")
			t.Cleanup(leaktest.Check(t))
			if err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
				r := makeRouterWithKey(logger, selfKey)
				s.SpawnBg(func() error { return utils.IgnoreCancel(r.Run(ctx)) })
				if err := r.WaitForStart(ctx); err != nil {
					return err
				}
				sub := r.peerManager.Subscribe()

				x := makeRouterWithKey(logger, tc.key)
				// WARNING: here we malform the router internal state.
				// It would be better to have a dedicated API for performing malformed handshakes.
				*x.nodeInfoProducer() = tc.info
				addr := TestAddress(r)
				tcpConn, err := x.dial(ctx, addr)
				if err != nil {
					return fmt.Errorf("peerTransport.dial(): %w", err)
				}
				defer tcpConn.Close()
				if tc.ok {
					if _, err := x.handshake(ctx, tcpConn, utils.Some(addr)); err != nil {
						return fmt.Errorf("handshake(): %w", err)
					}
					RequireUpdate(t, sub, PeerUpdate{
						NodeID: tc.info.NodeID,
						Status: PeerStatusUp,
					})
				} else {
					t.Logf("Expect immediate or delayed failure.")
					// Peer should drop the connection during handshake.
					conn, err := x.handshake(ctx, tcpConn, utils.Some(addr))
					if err != nil {
						return nil
					}
					if err := x.runConn(ctx, conn); utils.IgnoreCancel(err) == nil {
						return fmt.Errorf("want non-cancellation error, got %w", err)
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
	rng := utils.TestRng()
	t.Cleanup(leaktest.Check(t))

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		t.Logf("Set up and start the router.")
		r := makeRouter(logger, rng)
		s.SpawnBg(func() error { return utils.IgnoreCancel(r.Run(ctx)) })
		if err := r.WaitForStart(ctx); err != nil {
			return err
		}
		sub := r.peerManager.Subscribe()

		t.Logf("dial raw connections.")
		var peers []*Router
		var conns []*net.TCPConn
		addr := TestAddress(r)
		for range 10 {
			x := makeRouter(logger, rng)
			peers = append(peers, x)
			conn, err := x.dial(ctx, addr)
			if err != nil {
				return fmt.Errorf("x.dial(): %w", err)
			}
			defer conn.Close()
			conns = append(conns, conn)
		}
		t.Logf("Handshake the connections in reverse order.")
		for i := len(conns) - 1; i >= 0; i-- {
			if _, err := peers[i].handshake(ctx, conns[i], utils.Some(addr)); err != nil {
				return fmt.Errorf("handshake(): %w", err)
			}
			RequireUpdate(t, sub, PeerUpdate{
				NodeID: TestAddress(peers[i]).NodeID,
				Status: PeerStatusUp,
			})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRouter_dialPeer_Retry(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	rng := utils.TestRng()
	t.Cleanup(leaktest.Check(t))

	if err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		t.Logf("Set up and start the router.")
		r := makeRouter(logger, rng)
		s.SpawnBg(func() error { return utils.IgnoreCancel(r.Run(ctx)) })
		if err := r.WaitForStart(ctx); err != nil {
			return err
		}
		sub := r.peerManager.Subscribe()

		x := makeRouter(logger, rng)
		listener, err := tcp.Listen(x.Endpoint().AddrPort)
		if err != nil {
			return fmt.Errorf("tcp.Listen(): %w", err)
		}
		defer listener.Close()

		t.Log("Populate peer manager.")
		if err := r.AddAddrs(utils.Slice(TestAddress(x))); err != nil {
			return fmt.Errorf("r.AddAddrs(): %w", err)
		}

		t.Log("Accept and drop.")
		conn, err := listener.AcceptOrClose(ctx)
		if err != nil {
			return fmt.Errorf("peerTransport.dial(): %w", err)
		}
		conn.Close()

		t.Log("Accept and complete handshake.")
		conn, err = listener.AcceptOrClose(ctx)
		if err != nil {
			return fmt.Errorf("peerTransport.dial(): %w", err)
		}
		defer conn.Close()
		if _, err := x.handshake(ctx, conn, utils.None[NodeAddress]()); err != nil {
			return fmt.Errorf("handshake(): %w", err)
		}
		RequireUpdate(t, sub, PeerUpdate{
			NodeID: TestAddress(x).NodeID,
			Status: PeerStatusUp,
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRouter_dialPeer_Reject(t *testing.T) {
	rng := utils.TestRng()
	key := makeKey(rng)
	info := makeInfo(key)
	info2 := makeInfo(makeKey(rng))
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
			err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
				r := makeRouter(logger, rng)
				s.SpawnBg(func() error { return utils.IgnoreCancel(r.Run(ctx)) })
				if err := r.WaitForStart(ctx); err != nil {
					return err
				}

				x := makeRouter(logger, rng)
				listener, err := tcp.Listen(x.Endpoint().AddrPort)
				if err != nil {
					return fmt.Errorf("tcp.Listen(): %w", err)
				}
				defer listener.Close()
				if err := r.AddAddrs(utils.Slice(x.Endpoint().NodeAddress(tc.dialID))); err != nil {
					return fmt.Errorf("r.AddAddrs(): %w", err)
				}
				tcpConn, err := listener.AcceptOrClose(ctx)
				if err != nil {
					return fmt.Errorf("listener.AcceptOrClose(): %w", err)
				}
				defer tcpConn.Close()
				// Connections should be closed either during handshake, or immediately afterwards.
				*x.nodeInfoProducer() = tc.info
				conn, err := x.handshake(ctx, tcpConn, utils.None[NodeAddress]())
				if err != nil {
					return nil
				}
				if err := x.runConn(ctx, conn); !errors.Is(err, io.EOF) {
					return fmt.Errorf("want EOF, got %w", err)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRouter_dialPeers_Parallel(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	ctx := t.Context()
	rng := utils.TestRng()
	t.Cleanup(leaktest.Check(t))

	t.Logf("Set up and start the router.")
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		r := makeRouter(logger, rng)
		s.SpawnBg(func() error { return utils.IgnoreCancel(r.Run(ctx)) })
		if err := r.WaitForStart(ctx); err != nil {
			return err
		}
		sub := r.peerManager.Subscribe()

		t.Logf("Accept raw connections.")
		var peers []*Router
		var conns []*net.TCPConn
		for i := range 10 {
			t.Logf("ACCEPT %v", i)
			peer := makeRouter(logger, rng)
			listener, err := tcp.Listen(peer.Endpoint().AddrPort)
			if err != nil {
				return fmt.Errorf("tcp.Listen(): %w", err)
			}
			defer listener.Close()
			if err := r.AddAddrs(utils.Slice(TestAddress(peer))); err != nil {
				return fmt.Errorf("r.AddAddrs(): %w", err)
			}
			conn, err := listener.AcceptOrClose(ctx)
			if err != nil {
				return fmt.Errorf("listener.AcceptOrClose(): %w", err)
			}
			defer conn.Close()
			conns = append(conns, conn)
			peers = append(peers, peer)
		}
		t.Logf("Handshake the connections in reverse order.")
		for i := len(conns) - 1; i >= 0; i-- {
			conn := conns[i]
			peer := peers[i]
			if _, err := peer.handshake(ctx, conn, utils.None[NodeAddress]()); err != nil {
				return fmt.Errorf("handshake(): %w", err)
			}
			RequireUpdate(t, sub, PeerUpdate{
				NodeID: TestAddress(peer).NodeID,
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
	t.Cleanup(leaktest.Check(t))
	rng := utils.TestRng()
	if err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		r := makeRouter(logger, rng)
		s.SpawnBg(func() error { return utils.IgnoreCancel(r.Run(ctx)) })
		if err := r.WaitForStart(ctx); err != nil {
			return err
		}
		sub := r.peerManager.Subscribe()

		x := makeRouter(logger, rng)
		addr := TestAddress(r)
		tcpConn, err := x.dial(ctx, addr)
		if err != nil {
			return fmt.Errorf("dial(): %w", err)
		}
		defer tcpConn.Close()
		conn, err := x.handshake(ctx, tcpConn, utils.Some(addr))
		if err != nil {
			return fmt.Errorf("handshake(): %w", err)
		}
		peerID := TestAddress(x).NodeID
		RequireUpdate(t, sub, PeerUpdate{
			NodeID: peerID,
			Status: PeerStatusUp,
		})

		t.Log("Report the peer as bad.")
		r.Evict(peerID, errors.New("boom"))
		t.Log("Wait for conn down")
		if err := x.runConn(ctx, conn); !errors.Is(err, io.EOF) {
			return fmt.Errorf("want EOF, got %w", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRouter_DontSendOnInvalidChannel(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	t.Cleanup(leaktest.Check(t))
	rng := utils.TestRng()
	if err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		r := makeRouter(logger, rng)
		s.SpawnBg(func() error { return utils.IgnoreCancel(r.Run(ctx)) })
		if err := r.WaitForStart(ctx); err != nil {
			return err
		}
		sub := r.peerManager.Subscribe()

		desc1 := makeChDesc(1)
		r1, err := r.OpenChannel(desc1)
		if err != nil {
			return fmt.Errorf("r.OpenChannel(1): %w", err)
		}

		desc2 := makeChDesc(2)
		r2, err := r.OpenChannel(desc2)
		if err != nil {
			return fmt.Errorf("r.OpenChannel(2): %w", err)
		}

		x := makeRouter(logger, rng)
		x1, err := x.OpenChannel(desc1)
		if err != nil {
			return fmt.Errorf("x.OpenChannel(1): %w", err)
		}

		addr := TestAddress(r)
		tcpConn, err := x.dial(ctx, addr)
		if err != nil {
			return fmt.Errorf("dial(): %w", err)
		}
		defer tcpConn.Close()
		conn, err := x.handshake(ctx, tcpConn, utils.Some(addr))
		if err != nil {
			return fmt.Errorf("handshake(): %w", err)
		}
		RequireUpdate(t, sub, PeerUpdate{
			NodeID: TestAddress(x).NodeID,
			Status: PeerStatusUp,
		})
		s.SpawnBg(func() error { return mayDisconnectAfterDone(ctx, x.runConn(ctx, conn)) })
		n := 1
		msg1 := &TestMessage{Value: "Hello"}
		msg2 := &TestMessage{Value: "Hello2"}
		t.Log("Broadcast messages of both channels.")
		s.Spawn(func() error {
			for range n {
				r1.Broadcast(msg1)
				r2.Broadcast(msg2)
			}
			return nil
		})
		t.Log("Expect messages of 1 channel only.")
		for range n {
			got, err := x1.Recv(ctx)
			if err != nil {
				return fmt.Errorf("ReceiveMessage(): %w", err)
			}
			if err := utils.TestDiff[proto.Message](got.Message, msg1); err != nil {
				return fmt.Errorf("gotMsg: %v", err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

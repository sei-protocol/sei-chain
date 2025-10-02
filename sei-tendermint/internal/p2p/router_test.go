package p2p_test

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/tendermint/tendermint/internal/p2p"
	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/internal/p2p/p2ptest"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/types"
)

func echoReactor(ctx context.Context, channel *p2p.Channel) {
	iter := channel.Receive(ctx)
	for iter.Next(ctx) {
		envelope := iter.Envelope()
		value := envelope.Message.(*p2ptest.Message).Value
		if err := channel.Send(ctx, p2p.Envelope{
			To:      envelope.From,
			Message: &p2ptest.Message{Value: value},
		}); err != nil {
			return
		}
	}
}

func TestRouter_Network(t *testing.T) {
	ctx := t.Context()

	t.Cleanup(leaktest.Check(t))

	t.Logf("Create a test network and open a channel where all peers run echoReactor.")
	network := p2ptest.MakeNetwork(t, p2ptest.NetworkOptions{NumNodes: 8})
	local := network.RandomNode()
	peers := network.Peers(local.NodeID)
	channels := network.MakeChannels(t, chDesc)

	network.Start(t)

	channel := channels[local.NodeID]
	for _, peer := range peers {
		go echoReactor(ctx, channels[peer.NodeID])
	}

	t.Logf("Sending a message to each peer should work.")
	for _, peer := range peers {
		msg := &p2ptest.Message{Value: "foo"}
		p2ptest.RequireSend(t, channel, p2p.Envelope{To: peer.NodeID, Message: msg, ChannelID: chDesc.ID})
		p2ptest.RequireReceive(t, channel, p2p.Envelope{From: peer.NodeID, Message: msg, ChannelID: chDesc.ID})
	}

	t.Logf("Sending a broadcast should return back a message from all peers.")
	p2ptest.RequireSend(t, channel, p2p.Envelope{
		Broadcast: true,
		Message:   &p2ptest.Message{Value: "bar"},
	})
	expect := []*p2p.Envelope{}
	for _, peer := range peers {
		expect = append(expect, &p2p.Envelope{
			From:      peer.NodeID,
			ChannelID: 1,
			Message:   &p2ptest.Message{Value: "bar"},
		})
	}
	p2ptest.RequireReceiveUnordered(t, channel, expect)

	t.Logf("We then submit an error for a peer, and watch it get disconnected and")
	t.Logf("then reconnected as the router retries it.")
	peerUpdates := local.MakePeerUpdatesNoRequireEmpty(ctx, t)
	require.NoError(t, channel.SendError(ctx, p2p.PeerError{
		NodeID: peers[0].NodeID,
		Err:    errors.New("boom"),
	}))
	p2ptest.RequireUpdates(t, peerUpdates, []p2p.PeerUpdate{
		{NodeID: peers[0].NodeID, Status: p2p.PeerStatusDown},
		{NodeID: peers[0].NodeID, Status: p2p.PeerStatusUp},
	})
}

func TestRouter_Channel_Basic(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	logger, _ := log.NewDefaultLogger("plain", "debug")

	ctx := t.Context()

	// Set up a router with no transports (so no peers).
	peerManager, err := p2p.NewPeerManager(logger, selfID, dbm.NewMemDB(), p2p.PeerManagerOptions{}, p2p.NopMetrics())
	require.NoError(t, err)

	router, err := p2p.NewRouter(
		logger,
		p2p.NopMetrics(),
		selfKey,
		peerManager,
		func() *types.NodeInfo { return &selfInfo },
		nil,
		p2p.RouterOptions{
			Endpoint: p2p.Endpoint{tcp.TestReserveAddr()},
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
	chDesc2 := &p2p.ChannelDescriptor{ID: 2, MessageType: &p2ptest.Message{}}
	_, err = router.OpenChannel(chDesc2)
	require.NoError(t, err)

	t.Logf("We should be able to send on the channel, even though there are no peers.")
	p2ptest.RequireSend(t, channel, p2p.Envelope{
		To:      types.NodeID(strings.Repeat("a", 40)),
		Message: &p2ptest.Message{Value: "foo"},
	})

	t.Logf("A message to ourselves should be dropped.")
	p2ptest.RequireSend(t, channel, p2p.Envelope{
		To:      selfID,
		Message: &p2ptest.Message{Value: "self"},
	})
	p2ptest.RequireEmpty(t, channel)
}

// Channel tests are hairy to mock, so we use an in-memory network instead.
func TestRouter_Channel_SendReceive(t *testing.T) {
	ctx := t.Context()

	t.Cleanup(leaktest.Check(t))

	t.Logf("Create a test network and open a channel on all nodes.")
	network := p2ptest.MakeNetwork(t, p2ptest.NetworkOptions{NumNodes: 3})

	ids := network.NodeIDs()
	aID, bID, cID := ids[0], ids[1], ids[2]
	channels := network.MakeChannels(t, chDesc)
	a, b, c := channels[aID], channels[bID], channels[cID]
	otherChannels := network.MakeChannels(t, p2ptest.MakeChannelDesc(9))

	network.Start(t)

	t.Logf("Sending a message a->b should work, and not send anything further to a, b, or c.")
	p2ptest.RequireSend(t, a, p2p.Envelope{To: bID, Message: &p2ptest.Message{Value: "foo"}, ChannelID: chDesc.ID})
	p2ptest.RequireReceive(t, b, p2p.Envelope{From: aID, Message: &p2ptest.Message{Value: "foo"}, ChannelID: chDesc.ID})
	p2ptest.RequireEmpty(t, a, b, c)

	t.Logf("Sending a nil message a->b should be dropped.")
	p2ptest.RequireSend(t, a, p2p.Envelope{To: bID, Message: nil, ChannelID: chDesc.ID})
	p2ptest.RequireEmpty(t, a, b, c)

	t.Logf("Sending a different message type should be dropped.")
	p2ptest.RequireSend(t, a, p2p.Envelope{To: bID, Message: &gogotypes.BoolValue{Value: true}, ChannelID: chDesc.ID})
	p2ptest.RequireEmpty(t, a, b, c)

	t.Logf("Sending to an unknown peer should be dropped.")
	p2ptest.RequireSend(t, a, p2p.Envelope{
		To:        types.NodeID(strings.Repeat("a", 40)),
		Message:   &p2ptest.Message{Value: "a"},
		ChannelID: chDesc.ID,
	})
	p2ptest.RequireEmpty(t, a, b, c)

	t.Logf("Sending without a recipient should be dropped.")
	p2ptest.RequireSend(t, a, p2p.Envelope{Message: &p2ptest.Message{Value: "noto"}, ChannelID: chDesc.ID})
	p2ptest.RequireEmpty(t, a, b, c)

	t.Logf("Sending to self should be dropped.")
	p2ptest.RequireSend(t, a, p2p.Envelope{To: aID, Message: &p2ptest.Message{Value: "self"}, ChannelID: chDesc.ID})
	p2ptest.RequireEmpty(t, a, b, c)

	t.Logf("Removing b and sending to it should be dropped.")
	network.Remove(ctx, t, bID)
	p2ptest.RequireSend(t, a, p2p.Envelope{To: bID, Message: &p2ptest.Message{Value: "nob"}, ChannelID: chDesc.ID})
	p2ptest.RequireEmpty(t, a, b, c)

	t.Logf("After all this, sending a message c->a should work.")
	p2ptest.RequireSend(t, c, p2p.Envelope{To: aID, Message: &p2ptest.Message{Value: "bar"}, ChannelID: chDesc.ID})
	p2ptest.RequireReceive(t, a, p2p.Envelope{From: cID, Message: &p2ptest.Message{Value: "bar"}, ChannelID: chDesc.ID})
	p2ptest.RequireEmpty(t, a, b, c)

	t.Logf("None of these messages should have made it onto the other channels.")
	for _, other := range otherChannels {
		p2ptest.RequireEmpty(t, other)
	}
}

func TestRouter_Channel_Broadcast(t *testing.T) {
	t.Cleanup(leaktest.Check(t))

	ctx := t.Context()

	t.Logf("Create a test network and open a channel on all nodes.")
	network := p2ptest.MakeNetwork(t, p2ptest.NetworkOptions{NumNodes: 4})

	ids := network.NodeIDs()
	aID, bID, cID, dID := ids[0], ids[1], ids[2], ids[3]
	channels := network.MakeChannels(t, chDesc)
	a, b, c, d := channels[aID], channels[bID], channels[cID], channels[dID]

	network.Start(t)

	t.Logf("Sending a broadcast from b should work.")
	p2ptest.RequireSend(t, b, p2p.Envelope{Broadcast: true, Message: &p2ptest.Message{Value: "foo"}, ChannelID: chDesc.ID})
	p2ptest.RequireReceive(t, a, p2p.Envelope{From: bID, Message: &p2ptest.Message{Value: "foo"}, ChannelID: chDesc.ID})
	p2ptest.RequireReceive(t, c, p2p.Envelope{From: bID, Message: &p2ptest.Message{Value: "foo"}, ChannelID: chDesc.ID})
	p2ptest.RequireReceive(t, d, p2p.Envelope{From: bID, Message: &p2ptest.Message{Value: "foo"}, ChannelID: chDesc.ID})
	p2ptest.RequireEmpty(t, a, b, c, d)

	t.Logf("Removing one node from the network shouldn't prevent broadcasts from working.")
	network.Remove(ctx, t, dID)
	p2ptest.RequireSend(t, a, p2p.Envelope{Broadcast: true, Message: &p2ptest.Message{Value: "bar"}, ChannelID: chDesc.ID})
	p2ptest.RequireReceive(t, b, p2p.Envelope{From: aID, Message: &p2ptest.Message{Value: "bar"}, ChannelID: chDesc.ID})
	p2ptest.RequireReceive(t, c, p2p.Envelope{From: aID, Message: &p2ptest.Message{Value: "bar"}, ChannelID: chDesc.ID})
	p2ptest.RequireEmpty(t, a, b, c, d)
}

func TestRouter_Channel_Wrapper(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	t.Logf("Create a test network and open a channel on all nodes.")
	network := p2ptest.MakeNetwork(t, p2ptest.NetworkOptions{NumNodes: 2})

	ids := network.NodeIDs()
	aID, bID := ids[0], ids[1]
	chDesc := &p2p.ChannelDescriptor{
		ID:                  chID,
		MessageType:         &wrapperMessage{},
		Priority:            5,
		SendQueueCapacity:   10,
		RecvBufferCapacity:  10,
		RecvMessageCapacity: 10,
	}

	channels := network.MakeChannels(t, chDesc)
	a, b := channels[aID], channels[bID]

	network.Start(t)

	// Since wrapperMessage implements p2p.Wrapper and handles Message, it
	// should automatically wrap and unwrap sent messages -- we prepend the
	// wrapper actions to the message value to signal this.
	p2ptest.RequireSend(t, a, p2p.Envelope{To: bID, Message: &p2ptest.Message{Value: "foo"}, ChannelID: chDesc.ID})
	p2ptest.RequireReceive(t, b, p2p.Envelope{From: aID, Message: &p2ptest.Message{Value: "unwrap:wrap:foo"}, ChannelID: chDesc.ID})

	// If we send a different message that can't be wrapped, it should be dropped.
	p2ptest.RequireSend(t, a, p2p.Envelope{To: bID, Message: &gogotypes.BoolValue{Value: true}, ChannelID: chDesc.ID})
	p2ptest.RequireEmpty(t, b)

	// If we send the wrapper message itself, it should also be passed through
	// since WrapperMessage supports it, and should only be unwrapped at the receiver.
	p2ptest.RequireSend(t, a, p2p.Envelope{
		To:        bID,
		Message:   &wrapperMessage{Message: p2ptest.Message{Value: "foo"}},
		ChannelID: chDesc.ID,
	})
	p2ptest.RequireReceive(t, b, p2p.Envelope{
		From:      aID,
		Message:   &p2ptest.Message{Value: "unwrap:foo"},
		ChannelID: chDesc.ID,
	})

}

// WrapperMessage prepends the value with "wrap:" and "unwrap:" to test it.
type wrapperMessage struct {
	p2ptest.Message
}

var _ p2p.Wrapper = (*wrapperMessage)(nil)

func (w *wrapperMessage) Wrap(inner proto.Message) error {
	switch inner := inner.(type) {
	case *p2ptest.Message:
		w.Message.Value = fmt.Sprintf("wrap:%v", inner.Value)
	case *wrapperMessage:
		*w = *inner
	default:
		return fmt.Errorf("invalid message type %T", inner)
	}
	return nil
}

func (w *wrapperMessage) Unwrap() (proto.Message, error) {
	return &p2ptest.Message{Value: fmt.Sprintf("unwrap:%v", w.Message.Value)}, nil
}

func TestRouter_Channel_Error(t *testing.T) {
	t.Cleanup(leaktest.Check(t))

	ctx := t.Context()

	t.Logf("Create a test network and open a channel on all nodes.")
	network := p2ptest.MakeNetwork(t, p2ptest.NetworkOptions{NumNodes: 3})
	network.Start(t)

	ids := network.NodeIDs()
	aID, bID := ids[0], ids[1]
	channels := network.MakeChannels(t, chDesc)
	a := channels[aID]

	t.Logf("Erroring b should cause it to be disconnected. It will reconnect shortly after.")
	sub := network.Node(aID).MakePeerUpdates(ctx, t)
	p2ptest.RequireSendError(t, a, p2p.PeerError{NodeID: bID, Err: errors.New("boom")})
	p2ptest.RequireUpdates(t, sub, []p2p.PeerUpdate{
		{NodeID: bID, Status: p2p.PeerStatusDown},
		{NodeID: bID, Status: p2p.PeerStatusUp},
	})
}

type RouterHandle struct {
	filterByIP  atomic.Pointer[func(ctx context.Context, addr netip.AddrPort) error]
	router      *p2p.Router
	peerManager *p2p.PeerManager
}

var keyFiltered, infoFiltered = makeKeyAndInfo()

func spawnRouter(t *testing.T, logger log.Logger) *RouterHandle {
	t.Helper()
	ctx := t.Context()
	// Set up and start the router.
	opts := p2p.PeerManagerOptions{
		MinRetryTime: 100 * time.Millisecond,
	}
	peerManager, err := p2p.NewPeerManager(logger, selfID, dbm.NewMemDB(), opts, p2p.NopMetrics())
	require.NoError(t, err)
	r := RouterHandle{
		peerManager: peerManager,
	}
	router, err := p2p.NewRouter(
		logger,
		p2p.NopMetrics(),
		selfKey,
		peerManager,
		func() *types.NodeInfo { return &selfInfo },
		func(_ context.Context, id types.NodeID) error {
			if id == infoFiltered.NodeID {
				return errors.New("should filter")
			}
			return nil
		},
		p2p.RouterOptions{
			DialSleep:          func(context.Context) error { return nil },
			NumConcurrentDials: func() int { return 100 },
			FilterPeerByIP: func(ctx context.Context, addr netip.AddrPort) error {
				if f := r.filterByIP.Load(); f != nil {
					return (*f)(ctx, addr)
				}
				return nil
			},
			Endpoint: p2p.Endpoint{tcp.TestReserveAddr()},
			Connection: conn.DefaultMConnConfig(),
		},
	)
	require.NoError(t, err)
	require.NoError(t, router.Start(ctx))
	t.Cleanup(router.Stop)
	require.NoError(t, router.WaitForStart(ctx))
	r.router = router
	return &r
}

func TestRouter_FilterByIP(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	ctx := t.Context()
	t.Cleanup(leaktest.Check(t))

	h := spawnRouter(t, logger)
	sub := h.peerManager.Subscribe(ctx)

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		t.Logf("Connection should succeed.")
		key, info := makeKeyAndInfo()
		transport := p2p.TestTransport(logger.With("node",info.NodeID))
		s.SpawnBgNamed("transport.Run()", func() error { return transport.Run(ctx) })
		conn, err := transport.Dial(ctx, h.router.Endpoint())
		if err != nil {
			return fmt.Errorf("peerTransport.Dial(): %w", err)
		}
		defer conn.Close()
		if _, err := conn.Handshake(ctx, info, key); err != nil {
			return fmt.Errorf("conn.Handshake(): %w", err)
		}
		p2ptest.RequireUpdate(t, sub, p2p.PeerUpdate{
			NodeID: info.NodeID,
			Status: p2p.PeerStatusUp,
		})

		t.Logf("Enable filtering.")
		h.filterByIP.Store(utils.Alloc(func(ctx context.Context, addr netip.AddrPort) error {
			return errors.New("fail all")
		}))

		t.Logf("Connection should fail during handshake.")
		key, info = makeKeyAndInfo()
		transport = p2p.TestTransport(logger.With("node",info.NodeID))
		s.SpawnBgNamed("transport.Run()", func() error { return transport.Run(ctx) })
		conn, err = transport.Dial(ctx, h.router.Endpoint())
		if err != nil {
			return fmt.Errorf("peerTransport.Dial(): %w", err)
		}
		defer conn.Close()
		if _, err := conn.Handshake(ctx, info, key); err == nil {
			return fmt.Errorf("conn.Handshake(): expected error")
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
			ctx := t.Context()
			t.Cleanup(leaktest.Check(t))
			h := spawnRouter(t, logger)
			sub := h.peerManager.Subscribe(ctx)

			if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
				peerTransport := p2p.TestTransport(logger.With("node",peerID))
				s.SpawnBgNamed("peerTransport.Run()", func() error { return peerTransport.Run(ctx) })
				conn, err := peerTransport.Dial(ctx, h.router.Endpoint())
				if err != nil {
					return fmt.Errorf("peerTransport.Dial(): %w", err)
				}
				defer conn.Close()
				if tc.ok {
					if _, err := conn.Handshake(ctx, tc.info, tc.key); err != nil {
						return fmt.Errorf("conn.Handshake(): %w", err)
					}
					p2ptest.RequireUpdate(t, sub, p2p.PeerUpdate{
						NodeID: tc.info.NodeID,
						Status: p2p.PeerStatusUp,
					})
				} else {
					// Expect immediate or delayed failure.
					// Peer should drop the connection during handshake.
					if _, err := conn.Handshake(ctx, tc.info, tc.key); err != nil {
						return nil
					}
					if _, _, err := conn.ReceiveMessage(ctx); !errors.Is(err, context.Canceled) {
						return fmt.Errorf("want Canceled, got %w", err)
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

	t.Logf("Set up and start the router.")
	h := spawnRouter(t, logger)
	sub := h.peerManager.Subscribe(ctx)

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		t.Logf("Dial raw connections.")
		var conns []*p2p.Connection
		for range 10 {
			peerTransport := p2p.TestTransport(logger.With("node","peer"))
			s.SpawnBgNamed("peerTransport.Run()", func() error { return peerTransport.Run(ctx) })
			conn, err := peerTransport.Dial(ctx, h.router.Endpoint())
			if err != nil {
				return fmt.Errorf("peerTransport.Dial(): %w", err)
			}
			defer conn.Close()
			conns = append(conns, conn)
		}
		t.Logf("Handshake the connections in reverse order.")
		for i := len(conns) - 1; i >= 0; i-- {
			conn := conns[i]
			key, info := makeKeyAndInfo()
			if _, err := conn.Handshake(ctx, info, key); err != nil {
				return fmt.Errorf("conn.Handshake(): %w", err)
			}
			p2ptest.RequireUpdate(t, sub, p2p.PeerUpdate{
				NodeID: info.NodeID,
				Status: p2p.PeerStatusUp,
			})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRouter_DialPeer_Retry(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	ctx := t.Context()
	t.Cleanup(leaktest.Check(t))

	t.Logf("Set up and start the router.")
	h := spawnRouter(t, logger)
	sub := h.peerManager.Subscribe(ctx)

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		peerTransport := p2p.TestTransport(logger.With("node",peerID))
		s.SpawnBgNamed("peerTransport.Run()", func() error { return peerTransport.Run(ctx) })
		if err := peerTransport.WaitForStart(ctx); err != nil {
			return err
		}

		t.Log("Populate peer manager.")
		key, info := makeKeyAndInfo()
		if ok, err := h.peerManager.Add(peerTransport.Endpoint().NodeAddress(info.NodeID)); !ok || err != nil {
			return fmt.Errorf("peerManager.Add() = %v,%w", ok, err)
		}

		t.Log("Accept and drop.")
		conn, err := peerTransport.Accept(ctx)
		if err != nil {
			return fmt.Errorf("peerTransport.Dial(): %w", err)
		}
		conn.Close()

		t.Log("Accept and complete handshake.")
		conn, err = peerTransport.Accept(ctx)
		if err != nil {
			return fmt.Errorf("peerTransport.Dial(): %w", err)
		}
		defer conn.Close()
		if _, err := conn.Handshake(ctx, info, key); err != nil {
			return fmt.Errorf("conn.Handshake(): %w", err)
		}
		p2ptest.RequireUpdate(t, sub, p2p.PeerUpdate{
			NodeID: info.NodeID,
			Status: p2p.PeerStatusUp,
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

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
				peerTransport := p2p.TestTransport(logger.With("node",tc.dialID))
				s.SpawnBgNamed("peerTransport.Run()", func() error { return peerTransport.Run(ctx) })
				if err := peerTransport.WaitForStart(ctx); err != nil {
					return fmt.Errorf("peerTransport.WaitForStart(): %w", err)
				}
				if ok, err := h.peerManager.Add(peerTransport.Endpoint().NodeAddress(tc.dialID)); !ok || err != nil {
					return fmt.Errorf("peerManager.Add() = %v,%w", ok, err)
				}
				conn, err := peerTransport.Accept(ctx)
				if err != nil {
					return fmt.Errorf("peerTransport.Accept(): %w", err)
				}
				defer conn.Close()
				// Connections should be closed either during handshake, or immediately afterwards.
				if _, err := conn.Handshake(ctx, tc.info, key); err != nil {
					return nil
				}
				if _, _, err := conn.ReceiveMessage(ctx); !errors.Is(err, context.Canceled) {
					return fmt.Errorf("want Canceled, got %w", err)
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
		var conns []*p2p.Connection
		for i, info := range infos {
			t.Logf("ACCEPT %v %v", i, info.NodeID)
			peerTransport := p2p.TestTransport(logger.With("local", info.NodeID))
			s.SpawnBgNamed("peerTransport.Run()", func() error { return peerTransport.Run(ctx) })
			if err := peerTransport.WaitForStart(ctx); err != nil {
				return fmt.Errorf("peerTransport.WaitForStart(): %w", err)
			}
			if ok, err := h.peerManager.Add(peerTransport.Endpoint().NodeAddress(info.NodeID)); !ok || err != nil {
				return fmt.Errorf("peerManager.Add() = %v,%w", ok, err)
			}
			conn, err := peerTransport.Accept(ctx)
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
			if _, err := conn.Handshake(ctx, info, keys[i]); err != nil {
				return fmt.Errorf("conn.Handshake(): %w", err)
			}
			p2ptest.RequireUpdate(t, sub, p2p.PeerUpdate{
				NodeID: info.NodeID,
				Status: p2p.PeerStatusUp,
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
		peerTransport := p2p.TestTransport(logger.With("node",info.NodeID))
		s.SpawnBgNamed("peerTransport.Run()", func() error { return peerTransport.Run(ctx) })
		conn, err := peerTransport.Dial(ctx, h.router.Endpoint())
		if err != nil {
			return fmt.Errorf("peerTransport.Dial(): %w", err)
		}
		defer conn.Close()
		if _, err := conn.Handshake(ctx, info, key); err != nil {
			return fmt.Errorf("conn.Handshake(): %w", err)
		}
		p2ptest.RequireUpdate(t, sub, p2p.PeerUpdate{
			NodeID: info.NodeID,
			Status: p2p.PeerStatusUp,
		})

		t.Log("Report the peer as bad.")
		h.peerManager.Errored(info.NodeID, errors.New("boom"))
		p2ptest.RequireUpdate(t, sub, p2p.PeerUpdate{
			NodeID: info.NodeID,
			Status: p2p.PeerStatusDown,
		})
		t.Log("Wait for conn down")
		if _, _, err := conn.ReceiveMessage(ctx); !errors.Is(err, context.Canceled) {
			return fmt.Errorf("want Canceled, got %w", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func makeChDesc(id p2p.ChannelID) *p2p.ChannelDescriptor {
	return &p2p.ChannelDescriptor{
		ID:                  id,
		MessageType:         &p2ptest.Message{},
		Priority:            5,
		SendQueueCapacity:   10,
		RecvBufferCapacity:  10,
		RecvMessageCapacity: 10,
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
		peerTransport := p2p.TestTransport(logger.With("node",info.NodeID), desc1)
		s.SpawnBgNamed("peerTransport.Run()", func() error { return peerTransport.Run(ctx) })
		conn, err := peerTransport.Dial(ctx, h.router.Endpoint())
		if err != nil {
			return fmt.Errorf("peerTransport.Dial(): %w", err)
		}
		defer conn.Close()
		if _, err := conn.Handshake(ctx, info, key); err != nil {
			return fmt.Errorf("conn.Handshake(): %w", err)
		}
		p2ptest.RequireUpdate(t, sub, p2p.PeerUpdate{
			NodeID: info.NodeID,
			Status: p2p.PeerStatusUp,
		})
		n := 1
		msg1 := &p2ptest.Message{Value: "Hello"}
		msg2 := &p2ptest.Message{Value: "Hello2"}
		t.Log("Broadcast messages of both channels.")
		s.Spawn(func() error {
			for range n {
				if err := ch1.Send(ctx, p2p.Envelope{Broadcast: true, Message: msg1}); err != nil {
					return fmt.Errorf("ch1.Send(): %w", err)
				}
				if err := ch2.Send(ctx, p2p.Envelope{Broadcast: true, Message: msg2}); err != nil {
					return fmt.Errorf("ch2.Send(): %w", err)
				}
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
}

package p2p

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func makeKey(rng utils.Rng) NodeSecretKey {
	return NodeSecretKey(ed25519.TestSecretKey(utils.GenBytes(rng, 32)))
}

func makeNodeID(rng utils.Rng) types.NodeID {
	return makeKey(rng).Public().NodeID()
}

func makeAddrFor(rng utils.Rng, id types.NodeID) NodeAddress {
	return NodeAddress{
		NodeID:   id,
		Hostname: fmt.Sprintf("%s.example.com", utils.GenString(rng, 10)),
		Port:     uint16(rng.Int()),
	}
}

func makeAddr(rng utils.Rng) NodeAddress {
	return makeAddrFor(rng, makeNodeID(rng))
}

type fakeConn struct {
	info   peerConnInfo
	closed atomic.Bool
}

func makeConnFor(rng utils.Rng, id types.NodeID, dialing bool) *fakeConn {
	info := peerConnInfo{ID: id}
	if dialing {
		info.DialAddr = utils.Some(makeAddrFor(rng, id))
	}
	return &fakeConn{info: info}
}

func makeConnTo(addr NodeAddress) *fakeConn {
	return &fakeConn{
		info: peerConnInfo{
			ID:       addr.NodeID,
			DialAddr: utils.Some(addr),
		},
	}
}

func makeConn(rng utils.Rng, dialing bool) *fakeConn {
	return makeConnFor(rng, makeNodeID(rng), dialing)
}

func (c *fakeConn) Closed() bool {
	return c.closed.Load()
}

func (c *fakeConn) Info() peerConnInfo { return c.info }

func (c *fakeConn) Close() {
	c.closed.Store(true)
}

func makePeerManager(selfID types.NodeID, options *RouterOptions) *peerManager[*fakeConn] {
	return newPeerManager[*fakeConn](selfID, options)
}

var selfID = types.NodeIDFromPubKey(ed25519.TestSecretKey([]byte{12, 43}).Public())

func justIDs[C peerConn](conns connSet[C]) map[types.NodeID]bool {
	ids := map[types.NodeID]bool{}
	for id := range conns.All() {
		ids[id.NodeID] = true
	}
	return ids
}

func mustStartDial(t *testing.T, ctx context.Context, m *peerManager[*fakeConn]) []NodeAddress {
	addrs, err := m.StartDial(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, addrs)
	return addrs
}

func TestRouterOptions(t *testing.T) {
	rng := utils.TestRng()

	makeOpts := func(rng utils.Rng) RouterOptions {
		addrs := utils.Slice(makeAddr(rng), makeAddr(rng), makeAddr(rng))
		addrs2 := utils.Slice(makeAddr(rng))
		ids := utils.Slice(addrs[0].NodeID, addrs[2].NodeID)
		return RouterOptions{
			PersistentPeers:    addrs,
			BootstrapPeers:     addrs2,
			BlockSyncPeers:     ids,
			UnconditionalPeers: ids,
			PrivatePeers:       ids,
		}
	}
	optsOk := makeOpts(rng)
	optsBadPersistent := makeOpts(rng)
	optsBadPersistent.PersistentPeers[1].NodeID = "X"
	optsBadBootstrap := makeOpts(rng)
	optsBadBootstrap.BootstrapPeers[0].NodeID = "QQ"
	optsBadBlockSync := makeOpts(rng)
	optsBadBlockSync.BlockSyncPeers[1] = "Y"
	optsBadUnconditional := makeOpts(rng)
	optsBadUnconditional.UnconditionalPeers[0] = "Z"
	optsBadPrivate := makeOpts(rng)
	optsBadPrivate.PrivatePeers[1] = "W"
	testcases := map[string]struct {
		options RouterOptions
		ok      bool
	}{
		"empty":                  {RouterOptions{}, true},
		"valid":                  {optsOk, true},
		"bad PersistentPeers":    {optsBadPersistent, false},
		"bad BootstrapPeers":     {optsBadBootstrap, false},
		"bad BlockSyncPeers":     {optsBadBlockSync, false},
		"bad UnconditionalPeers": {optsBadUnconditional, false},
		"bad PrivatePeers":       {optsBadPrivate, false},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			err := tc.options.Validate()
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestPeerManager_PushPex(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()

	t.Log("Generate persistent and bootstrap peers")
	persistentAddrs := utils.GenSliceN(rng, 2, makeAddr)
	bootstrapAddrs := utils.GenSliceN(rng, 4, makeAddr)
	unconditionalPeer := makeNodeID(rng)

	m := makePeerManager(selfID, &RouterOptions{
		BootstrapPeers:     bootstrapAddrs,
		PersistentPeers:    persistentAddrs,
		UnconditionalPeers: utils.Slice(unconditionalPeer),
		MaxInbound:         utils.Some(50),
		MaxOutbound:        utils.Some(50),
	})

	t.Log("All configured peers should eventually become dialable")
	want := map[types.NodeID]bool{}
	for _, addr := range append(slices.Clone(persistentAddrs), bootstrapAddrs...) {
		want[addr.NodeID] = true
	}
	seen := map[types.NodeID]bool{}
	for len(seen) < len(want) {
		addrs := mustStartDial(t, ctx, m)
		id := addrs[0].NodeID
		require.True(t, want[id])
		seen[id] = true
	}

	t.Log("Pushing new addresses via PEX should make them immediately dialable")
	newPeer := makeAddr(rng)
	require.NoError(t, m.PushPex(utils.Some(makeNodeID(rng)), utils.Slice(newPeer)))
	require.Equal(t, utils.Slice(newPeer), mustStartDial(t, ctx, m))

	t.Log("DialFailed makes the peer eligible for dialing again")
	m.DialFailed(newPeer.NodeID)
	require.Equal(t, utils.Slice(newPeer), mustStartDial(t, ctx, m))

	t.Log("Unconditional peers can always connect inbound")
	require.NoError(t, m.Connected(makeConnFor(rng, unconditionalPeer, false)))
}

func TestPeerManager_ConcurrentDials(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	addrs := utils.GenSliceN(rng, 4, makeAddr)
	addrSet := map[types.NodeID]bool{}
	for _, addr := range addrs {
		addrSet[addr.NodeID] = true
	}
	m := makePeerManager(makeNodeID(rng), &RouterOptions{
		BootstrapPeers: addrs,
		MaxOutbound:    utils.Some(len(addrs)),
	})
	dialing := utils.NewMutex(map[types.NodeID]bool{})
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for range 20 {
			addrs, err := m.StartDial(ctx)
			if err != nil {
				return fmt.Errorf("m.StartDial(): %w", err)
			}
			addr := addrs[0]
			if !addrSet[addr.NodeID] {
				return fmt.Errorf("unexpected addr %v", addr)
			}
			for dialing := range dialing.Lock() {
				if dialing[addr.NodeID] {
					return fmt.Errorf("duplicate concurrent dials for %v", addr.NodeID)
				}
				dialing[addr.NodeID] = true
			}
			s.Spawn(func() error {
				if err := utils.Sleep(ctx, 50*time.Millisecond); err != nil {
					return err
				}
				for dialing := range dialing.Lock() {
					delete(dialing, addr.NodeID)
				}
				m.DialFailed(addr.NodeID)
				return nil
			})
		}
		return nil
	})
	require.NoError(t, err)
}

// Test checking that all the provided addresses are eventually dialed.
func TestPeerManager_DialRoundRobin(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	addrsMap := map[NodeAddress]bool{}
	var persistentAddrs []NodeAddress
	var bootstrapAddrs []NodeAddress
	for _, addrs := range utils.Slice(&persistentAddrs, &bootstrapAddrs) {
		for range 10 {
			id := makeNodeID(rng)
			addr := makeAddrFor(rng, id)
			addrsMap[addr] = true
			*addrs = append(*addrs, addr)
		}
	}
	m := makePeerManager(makeNodeID(rng), &RouterOptions{
		BootstrapPeers:  bootstrapAddrs,
		PersistentPeers: persistentAddrs,
	})
	for len(addrsMap) > 0 {
		addr := mustStartDial(t, ctx, m)[0]
		delete(addrsMap, addr)
		m.DialFailed(addr.NodeID)
	}
}

// Test checking that MaxConnected limit applies to non-uncondional peers.
func TestPeerManager_MaxConnected(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	const maxIn = 3
	const maxOut = 4

	unconditionalPeers := utils.GenSliceN(rng, 3, makeNodeID)
	persistentPeers := utils.GenSliceN(rng, 2, makeAddr)
	bootstrapPeers := utils.GenSliceN(rng, maxOut*2, makeAddr)
	m := makePeerManager(makeNodeID(rng), &RouterOptions{
		PersistentPeers:    persistentPeers,
		BootstrapPeers:     bootstrapPeers,
		UnconditionalPeers: unconditionalPeers,
		MaxInbound:         utils.Some(maxIn),
		MaxOutbound:        utils.Some(maxOut),
	})

	want := map[types.NodeID]bool{}
	t.Log("outbound connections up to MaxOutbound succeed")
	for range maxOut {
		addrs := mustStartDial(t, ctx, m)
		conn := makeConnTo(addrs[0])
		require.NoError(t, m.Connected(conn))
		want[conn.Info().ID] = true
	}

	t.Log("inbound regular connections are limited by MaxInbound")
	for range maxIn {
		conn := makeConn(rng, false)
		require.NoError(t, m.Connected(conn))
		want[conn.Info().ID] = true
	}
	require.ErrorIs(t, m.Connected(makeConn(rng, false)), errTooManyPeers)

	t.Log("unconditional peers bypass inbound limits")
	for _, id := range unconditionalPeers {
		conn := makeConnFor(rng, id, false)
		require.NoError(t, m.Connected(conn))
		want[id] = true
	}

	require.NoError(t, utils.TestDiff(want, justIDs(m.Conns())))
}

// Test checking that concurrent dialing is limited by the number of outbound slots.
// I.e. dialing + connected <= MaxOutbound.
func TestPeerManager_MaxConnectedForDial(t *testing.T) {
	ctx := t.Context()
	maxOut := 10
	rng := utils.TestRng()

	addrs := utils.GenSliceN(rng, maxOut*2, makeAddr)
	m := makePeerManager(makeNodeID(rng), &RouterOptions{
		BootstrapPeers: addrs,
		MaxOutbound:    utils.Some(maxOut),
	})
	var conns []*fakeConn
	for range maxOut {
		addrs := mustStartDial(t, ctx, m)
		conn := makeConnTo(addrs[0])
		require.NoError(t, m.Connected(conn))
		conns = append(conns, conn)
	}
	var dialsAndConns atomic.Int64
	dialsAndConns.Store(int64(maxOut))
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			// Gradually disconnect existing connections.
			for _, conn := range conns {
				if err := utils.Sleep(ctx, 50*time.Millisecond); err != nil {
					return err
				}
				dialsAndConns.Add(-1)
				m.Disconnected(conn)
			}
			return nil
		})
		for range 5 * maxOut {
			// Dial peers as fast as possible.
			addrs, err := m.StartDial(ctx)
			if err != nil {
				return fmt.Errorf("m.StartDial(): %w", err)
			}
			if got := int(dialsAndConns.Add(1)); got > maxOut {
				return fmt.Errorf("dials + conns = %v, want <= %v", got, maxOut)
			}
			id := addrs[0].NodeID
			s.Spawn(func() error {
				if err := utils.Sleep(ctx, 20*time.Millisecond); err != nil {
					return err
				}
				dialsAndConns.Add(-1)
				m.DialFailed(id)
				return nil
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPeerManager_MaxOutboundConnectionsForDialing(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	maxOutbound := 3

	var addrs []NodeAddress
	for range maxOutbound * 4 {
		addrs = append(addrs, makeAddr(rng))
	}
	m := makePeerManager(makeNodeID(rng), &RouterOptions{
		BootstrapPeers: addrs,
		MaxOutbound:    utils.Some(maxOutbound),
	})

	var dialsAndConns atomic.Int64
	const attempts = 20
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for i := range attempts {
			addrs, err := m.StartDial(ctx)
			if err != nil {
				return fmt.Errorf("m.StartDial(): %w", err)
			}
			if got := int(dialsAndConns.Add(1)); got > maxOutbound {
				return fmt.Errorf("dialing + outbound = %v, want <= %v", got, maxOutbound)
			}
			addr := addrs[0]
			s.Spawn(func() error {
				if i%2 == 0 {
					if err := utils.Sleep(ctx, 10*time.Millisecond); err != nil {
						return err
					}
					// Keep accounting in sync with slot release: decrement before
					// unblocking StartDial to avoid transient overcount in this test.
					dialsAndConns.Add(-1)
					m.DialFailed(addr.NodeID)
					return nil
				}
				conn := makeConnTo(addr)
				if err := m.Connected(conn); err != nil {
					dialsAndConns.Add(-1)
					return err
				}
				if err := utils.Sleep(ctx, 20*time.Millisecond); err != nil {
					dialsAndConns.Add(-1)
					m.Disconnected(conn)
					return err
				}
				dialsAndConns.Add(-1)
				m.Disconnected(conn)
				return nil
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPeerManager_AcceptsInboundWhenOutboundFull(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	maxOutbound := 2
	maxConns := 7

	var addrs []NodeAddress
	for range maxConns {
		addrs = append(addrs, makeAddr(rng))
	}
	m := makePeerManager(makeNodeID(rng), &RouterOptions{
		BootstrapPeers: addrs,
		MaxOutbound:    utils.Some(maxOutbound),
		MaxInbound:     utils.Some(maxConns - maxOutbound),
	})
	// Fill up outbound slots.
	for range maxOutbound {
		addrs := mustStartDial(t, ctx, m)
		require.NoError(t, m.Connected(makeConnTo(addrs[0])))
	}
	require.Equal(t, maxOutbound, m.Conns().Len())
	// Fill up inbound slots.
	for range maxConns - maxOutbound {
		require.NoError(t, m.Connected(makeConn(rng, false)))
	}
	require.Equal(t, maxConns, m.Conns().Len())
}

// Test checking that StartDial will wake up whenever address can be dialed.
func TestPeerManager_Wake(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	maxConns := 2
	persistentAddr := makeAddr(rng)
	m := makePeerManager(makeNodeID(rng), &RouterOptions{
		PersistentPeers: utils.Slice(persistentAddr),
		MaxOutbound:     utils.Some(maxConns),
		MaxInbound:      utils.Some(maxConns),
	})
	// Adding an address while none are available should wake.
	require.True(t, utils.MonitorWatchUpdates(&m.inner, func() {
		require.NoError(t, m.PushPex(utils.Some(makeNodeID(rng)), utils.Slice(makeAddr(rng))))
	}))
	t.Log("freeing a dial slot via DialFailed should wake")
	addrs := mustStartDial(t, ctx, m)
	require.True(t, utils.MonitorWatchUpdates(&m.inner, func() {
		m.DialFailed(addrs[0].NodeID)
	}))

	t.Log("establishing a connection should wake")
	addrs = mustStartDial(t, ctx, m)
	conn := makeConnTo(addrs[0])
	require.True(t, utils.MonitorWatchUpdates(&m.inner, func() {
		require.NoError(t, m.Connected(conn))
	}))

	t.Log("disconnecting should wake")
	require.True(t, utils.MonitorWatchUpdates(&m.inner, func() {
		m.Disconnected(conn)
	}))
}

// Test checking that manager closes duplicate inbound connection.
func TestPeerManager_DuplicateConn(t *testing.T) {
	rng := utils.TestRng()
	ids := utils.GenSliceN(rng, 10, makeNodeID)
	m := makePeerManager(makeNodeID(rng), &RouterOptions{
		MaxInbound: utils.Some(len(ids) + 5),
	})
	for _, id := range ids {
		var active *fakeConn
		for range 5 {
			conn := makeConnFor(rng, id, false)
			require.NoError(t, m.Connected(conn))
			if active != nil {
				require.True(t, active.Closed())
			}
			active = conn
			require.False(t, active.Closed())
		}
	}
}

func TestPeerManager_Subscribe(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	maxConns := 60
	m := makePeerManager(makeNodeID(rng), &RouterOptions{
		MaxInbound:  utils.Some(maxConns),
		MaxOutbound: utils.Some(maxConns),
	})
	t.Log("initialize with some connections")
	for range 5 {
		require.NoError(t, m.Connected(makeConn(rng, false)))
	}
	t.Log("subscribe with preexisting connections")
	recv := m.Subscribe()
	got := map[types.NodeID]bool{}
	for range 100 {
		// Modify connections.
		for range 10 {
			conns := m.Conns()
			if conns.Len() == 0 || (conns.Len() < maxConns && utils.GenBool(rng)) {
				require.NoError(t, m.Connected(makeConn(rng, false)))
			} else {
				for _, conn := range conns.All() {
					m.Disconnected(conn)
					break
				}
			}
		}
		// Check updates.
		conns := m.Conns()
		for len(got) != conns.Len() || utils.TestDiff(justIDs(conns), got) != nil {
			update, err := recv.Recv(ctx)
			require.NoError(t, err)
			switch update.Status {
			case PeerStatusUp:
				got[update.NodeID] = true
			case PeerStatusDown:
				delete(got, update.NodeID)
			}
		}
	}
}

package p2p

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

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

func justIDs[C peerConn](conns connSet[C]) map[types.NodeID]bool {
	ids := map[types.NodeID]bool{}
	for id := range conns.All() {
		ids[id] = true
	}
	return ids
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

func TestPeerManager_AddAddrs(t *testing.T) {
	rng := utils.TestRng()

	t.Log("Generate some addresses")
	var ids []types.NodeID
	for range 6 {
		ids = append(ids, makeNodeID(rng))
	}
	addrs := map[types.NodeID][]NodeAddress{}
	for _, id := range ids {
		for range rng.Intn(3) + 2 {
			addrs[id] = append(addrs[id], makeAddrFor(rng, id))
		}
	}

	t.Log("Collect all persistent peers' addrs.")
	var persistentAddrs []NodeAddress
	for _, id := range ids[:2] {
		persistentAddrs = append(persistentAddrs, addrs[id]...)
	}
	t.Log("Collect some other peers' addrs.")
	var bootstrapAddrs []NodeAddress
	for _, id := range ids[2:] {
		bootstrapAddrs = append(bootstrapAddrs, addrs[id][:2]...)
	}
	unconditionalPeer := makeNodeID(rng)

	t.Log("Create peer manager.")
	maxPeers := 10
	m := makePeerManager(selfID, &RouterOptions{
		BootstrapPeers:  bootstrapAddrs,
		PersistentPeers: persistentAddrs,
		// Unconditional peers are just persistent peers that don't need to be dialed.
		UnconditionalPeers: utils.Slice(unconditionalPeer),
		// Blocksync peers are a subset of persistent peers.
		// It is also a valid configuration to add blocksync peer without adding
		// an address to persistent peers, but in such a case we expect such a peer to
		// connect to us instead.
		BlockSyncPeers: utils.Slice(ids[1]),
		PrivatePeers:   utils.Slice(ids[4]),
		MaxPeers:       utils.Some(maxPeers),
	})

	t.Log("Check that all expected addrs are present.")
	for _, id := range ids[:2] {
		require.ElementsMatch(t, addrs[id], m.Addresses(id))
	}
	for _, id := range ids[2:] {
		require.ElementsMatch(t, addrs[id][:2], m.Addresses(id))
	}
	require.ElementsMatch(t, []NodeAddress{}, m.Addresses(unconditionalPeer))

	t.Log("Add all addresses at once.")
	var allAddrs []NodeAddress
	for _, id := range ids {
		allAddrs = append(allAddrs, addrs[id]...)
	}
	require.NoError(t, m.AddAddrs(allAddrs))

	t.Log("Check that all expected addrs are present.")
	for _, id := range ids {
		require.ElementsMatch(t, addrs[id], m.Addresses(id))
	}
	require.ElementsMatch(t, []NodeAddress{}, m.Addresses(unconditionalPeer))

	t.Log("Check that adding new persistent peer address is ignored.")
	require.NoError(t, m.AddAddrs(utils.Slice(makeAddrFor(rng, ids[0]))))
	require.ElementsMatch(t, addrs[ids[0]], m.Addresses(ids[0]))
	require.NoError(t, m.AddAddrs(utils.Slice(makeAddrFor(rng, unconditionalPeer))))
	require.ElementsMatch(t, []NodeAddress{}, m.Addresses(unconditionalPeer))

	t.Log("Check that maxAddrsPerPeer limit is respected")
	idWithManyAddrs := ids[2]
	for range maxAddrsPerPeer {
		addrs[idWithManyAddrs] = append(addrs[idWithManyAddrs], makeAddrFor(rng, idWithManyAddrs))
	}
	require.NoError(t, m.AddAddrs(addrs[idWithManyAddrs]))
	// WARNING: here we implicitly assume that addresses are added in order.
	require.ElementsMatch(t, addrs[idWithManyAddrs][:maxAddrsPerPeer], m.Addresses(idWithManyAddrs))

	t.Log("Check that options.MaxPeers limit is respected")
	var newAddrs []NodeAddress
	for range maxPeers {
		addr := makeAddr(rng)
		ids = append(ids, addr.NodeID)
		addrs[addr.NodeID] = utils.Slice(addr)
		newAddrs = append(newAddrs, addr)
	}
	require.NoError(t, m.AddAddrs(newAddrs))
	expectedIDs := maxPeers + 2 // There are 2 persistent peers.
	for _, id := range ids[:expectedIDs] {
		expectedAddrs := min(len(addrs[id]), maxAddrsPerPeer)
		require.ElementsMatch(t, addrs[id][:expectedAddrs], m.Addresses(id))
	}
	for _, id := range ids[expectedIDs:] {
		require.ElementsMatch(t, nil, m.Addresses(id))
	}

	t.Log("Check that failed addresses are replaceable")
	m.DialFailed(addrs[idWithManyAddrs][1]) // we fail 1 arbitrary adderess
	newAddrs = addrs[idWithManyAddrs][maxAddrsPerPeer:]
	require.NoError(t, m.AddAddrs(newAddrs)) // we try to add some addrs
	want := append([]NodeAddress(nil), addrs[idWithManyAddrs][:maxAddrsPerPeer]...)
	want[1] = newAddrs[0] // the first one newly added should replace the failed one.
	require.ElementsMatch(t, want, m.Addresses(idWithManyAddrs))

	t.Log("Check that failed peers are replaceable")
	for _, addr := range addrs[ids[4]] {
		m.DialFailed(addr)
	}
	newPeer := makeAddr(rng)
	require.NoError(t, m.AddAddrs(utils.Slice(newPeer)))
	require.ElementsMatch(t, nil, m.Addresses(ids[4]))
	require.ElementsMatch(t, utils.Slice(newPeer), m.Addresses(newPeer.NodeID))
}

func TestPeerManager_ConcurrentDials(t *testing.T) {
	ctx := t.Context()
	for _, tc := range []struct{ peers, maxDials, dials int }{
		{peers: 10, maxDials: 3, dials: 20}, // dialing limited by MaxConcurrentDials
		{peers: 4, maxDials: 10, dials: 20}, // dialing limited by available peer addrs
	} {
		t.Run(fmt.Sprintf("peers=%v maxDials=%v", tc.peers, tc.maxDials), func(t *testing.T) {
			rng := utils.TestRng()
			addrsMap := map[NodeAddress]bool{}
			// Generate some persistent and non-persistent peers.
			var bootstrapAddrs []NodeAddress
			var persistentAddrs []NodeAddress
			for i := range tc.peers {
				addrs := &bootstrapAddrs
				if i%2 == 0 {
					addrs = &persistentAddrs
				}
				addr := makeAddr(rng)
				*addrs = append(*addrs, addr)
				addrsMap[addr] = (addrs == &persistentAddrs)
			}
			m := makePeerManager(makeNodeID(rng), &RouterOptions{
				BootstrapPeers:     bootstrapAddrs,
				PersistentPeers:    persistentAddrs,
				MaxConcurrentDials: utils.Some(tc.maxDials),
				MaxConnected:       utils.Some(tc.dials),
			})
			dialing := utils.NewMutex(map[types.NodeID]bool{})
			err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
				for i := range tc.dials {
					persistentPeer := i%2 == 0
					addr, err := m.StartDial(ctx, persistentPeer)
					if err != nil {
						return fmt.Errorf("StartDial(): %w", err)
					}
					if isPersistent, ok := addrsMap[addr]; !ok {
						return fmt.Errorf("unexpected addr %v", addr)
					} else if isPersistent != persistentPeer {
						return fmt.Errorf("address does not match the requested type (peristent/non-persistent)")
					}
					for dialing := range dialing.Lock() {
						if got := len(dialing); got > tc.maxDials {
							return fmt.Errorf("dials limit exceeded: %v", got)
						}
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
						m.DialFailed(addr)
						return nil
					})
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
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
			for range rng.Intn(5) + 1 {
				addr := makeAddrFor(rng, id)
				addrsMap[addr] = true
				*addrs = append(*addrs, addr)
			}
		}
	}
	m := makePeerManager(makeNodeID(rng), &RouterOptions{
		BootstrapPeers:  bootstrapAddrs,
		PersistentPeers: persistentAddrs,
	})
	for i := 0; len(addrsMap) > 0; i++ {
		persistentPeer := i%2 == 0
		addr, err := m.StartDial(ctx, persistentPeer)
		require.NoError(t, err)
		delete(addrsMap, addr)
		m.DialFailed(addr)
	}
}

// Test checking that MaxConnected limit applies to non-uncondional peers.
func TestPeerManager_MaxConnected(t *testing.T) {
	ctx := t.Context()
	maxConns := 5
	rng := utils.TestRng()

	// Generate some unconditional peers (persistent peers are also unconditional)
	isUnconditional := map[types.NodeID]bool{}
	var unconditionalPeers []types.NodeID
	var persistentPeers []NodeAddress
	for range 20 {
		addr := makeAddr(rng)
		isUnconditional[addr.NodeID] = true
		if utils.GenBool(rng) {
			unconditionalPeers = append(unconditionalPeers, addr.NodeID)
		} else {
			persistentPeers = append(persistentPeers, addr)
		}
	}

	m := makePeerManager(makeNodeID(rng), &RouterOptions{
		PersistentPeers:    persistentPeers,
		UnconditionalPeers: unconditionalPeers,
		MaxConnected:       utils.Some(maxConns),
	})

	// Construct connections to all unconditional peers and some regular peers.
	want := map[types.NodeID]bool{}
	for i := range max(len(persistentPeers), len(unconditionalPeers), maxConns+5) {
		// One connection to persistent peer.
		if i < len(persistentPeers) {
			addr, err := m.StartDial(ctx, true)
			require.NoError(t, err)
			conn := makeConnFor(rng, addr.NodeID, utils.GenBool(rng))
			require.NoError(t, m.Connected(conn))
			want[addr.NodeID] = true
		}

		// One connection to unconditional peer.
		if i < len(unconditionalPeers) {
			id := unconditionalPeers[i]
			conn := makeConnFor(rng, id, utils.GenBool(rng))
			require.NoError(t, m.Connected(conn))
			want[id] = true
		}

		// One connection to regular peer.
		conn := makeConn(rng, utils.GenBool(rng))
		wantErr := i >= maxConns
		if err := m.Connected(conn); (err != nil) != wantErr {
			t.Fatalf("m.Connected() = %v, wantErr = %v", err, wantErr)
		}
		if !wantErr {
			want[conn.Info().ID] = true
		}

		// Check if connection sets match.
		if err := utils.TestDiff(want, justIDs(m.Conns())); err != nil {
			t.Fatal(fmt.Errorf("m.Conns() %w", err))
		}
	}
}

// Test checking that concurrent dialing is limited by the number of connection slots.
// I.e. dialing + connected <= MaxConnected.
func TestPeerManager_MaxConnectedForDial(t *testing.T) {
	ctx := t.Context()
	maxConns := 10
	rng := utils.TestRng()

	var addrs []NodeAddress
	for range maxConns {
		addrs = append(addrs, makeAddr(rng))
	}
	m := makePeerManager(makeNodeID(rng), &RouterOptions{
		BootstrapPeers:     addrs,
		MaxConcurrentDials: utils.Some(maxConns),
		MaxConnected:       utils.Some(maxConns),
	})
	var conns []*fakeConn
	for range maxConns {
		conn := makeConn(rng, false)
		conns = append(conns, conn)
		require.NoError(t, m.Connected(conn))
	}
	var dialsAndConns atomic.Int64
	dialsAndConns.Store(int64(maxConns))
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
		for range maxConns {
			// Dial peers as fast as possible.
			_, err := m.StartDial(ctx, false)
			if err != nil {
				return fmt.Errorf("m.StartDial(): %w", err)
			}
			if got := int(dialsAndConns.Add(1)); got > maxConns {
				return fmt.Errorf("dials + conns = %v, want <= %v", got, maxConns)
			}
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
		BootstrapPeers:         addrs,
		MaxPeers:               utils.Some(len(addrs)),
		MaxConcurrentDials:     utils.Some(len(addrs)),
		MaxConnected:           utils.Some(len(addrs)),
		MaxOutboundConnections: utils.Some(maxOutbound),
	})

	var dialsAndConns atomic.Int64
	const attempts = 20
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for i := range attempts {
			addr, err := m.StartDial(ctx, false)
			if err != nil {
				return fmt.Errorf("m.StartDial(): %w", err)
			}
			if got := int(dialsAndConns.Add(1)); got > maxOutbound {
				return fmt.Errorf("dialing + outbound = %v, want <= %v", got, maxOutbound)
			}
			s.Spawn(func() error {
				defer dialsAndConns.Add(-1)
				if i%2 == 0 {
					if err := utils.Sleep(ctx, 10*time.Millisecond); err != nil {
						return err
					}
					m.DialFailed(addr)
					return nil
				}
				conn := makeConnTo(addr)
				if err := m.Connected(conn); err != nil {
					return err
				}
				defer m.Disconnected(conn)
				if err := utils.Sleep(ctx, 20*time.Millisecond); err != nil {
					return err
				}
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
		BootstrapPeers:         addrs,
		MaxConcurrentDials:     utils.Some(maxConns),
		MaxConnected:           utils.Some(maxConns),
		MaxOutboundConnections: utils.Some(maxOutbound),
	})
	// Fill up outbound slots.
	for range maxOutbound {
		addr := utils.OrPanic1(m.StartDial(ctx, false))
		require.NoError(t, m.Connected(makeConnTo(addr)))
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
	maxDials := 1
	maxConns := 2
	persistentAddr := makeAddr(rng)
	m := makePeerManager(makeNodeID(rng), &RouterOptions{
		PersistentPeers:    utils.Slice(persistentAddr),
		MaxConcurrentDials: utils.Some(maxDials),
		MaxConnected:       utils.Some(maxConns),
	})
	// Adding an address while none are available should wake.
	addrs := utils.Slice(makeAddr(rng))
	require.True(t, utils.MonitorWatchUpdates(&m.inner, func() {
		require.NoError(t, m.AddAddrs(addrs))
	}))
	// Adding duplicate address should NOT wake.
	require.False(t, utils.MonitorWatchUpdates(&m.inner, func() {
		require.NoError(t, m.AddAddrs(addrs))
	}))
	conns := map[bool]*fakeConn{}
	for _, persistentPeer := range utils.Slice(false, true) {
		// Freeing a dial slot via DialFailed should wake.
		addr, err := m.StartDial(ctx, persistentPeer)
		require.NoError(t, err)
		require.True(t, utils.MonitorWatchUpdates(&m.inner, func() {
			m.DialFailed(addr)
		}))
		// Freeing a dial slot via Connected should wake.
		addr, err = m.StartDial(ctx, persistentPeer)
		require.NoError(t, err)
		conns[persistentPeer] = makeConnTo(addr)
		require.True(t, utils.MonitorWatchUpdates(&m.inner, func() {
			require.NoError(t, m.Connected(conns[persistentPeer]))
		}))
	}
	// Fill all the connection slots.
	for m.Conns().Len() < maxConns {
		require.NoError(t, m.Connected(makeConn(rng, false)))
	}
	// Freeing a connection slot via Disconnected should wake (as long as there are addresses to dial),
	// since we don't dial if connections are full (for non-persistent connections).
	require.True(t, utils.MonitorWatchUpdates(&m.inner, func() {
		m.Disconnected(conns[false])
	}))
}

// Test checking that manager does not allow for duplicate connections,
// and that it closes the duplicates.
func TestPeerManager_DuplicateConn(t *testing.T) {
	rng := utils.TestRng()
	var addrs []NodeAddress
	var persistentAddrs []NodeAddress
	for range 10 {
		addr := makeAddr(rng)
		addrs = append(addrs, addr)
		if utils.GenBool(rng) {
			persistentAddrs = append(persistentAddrs, addr)
		}
	}
	m := makePeerManager(makeNodeID(rng), &RouterOptions{
		PersistentPeers: persistentAddrs,
	})
	for _, addr := range addrs {
		var active utils.Option[*fakeConn]
		for range 5 {
			conn := makeConnFor(rng, addr.NodeID, utils.GenBool(rng))
			toClose := utils.Some(conn)
			// Peer manager has internal logic deciding whether a new connection should replace the old one (err == nil) or not.
			// However at most 1 connection to each peer should be active at all times.
			if err := m.Connected(conn); err == nil {
				active, toClose = toClose, active
			}
			activeConn, ok := active.Get()
			require.True(t, ok)
			require.False(t, activeConn.Closed())
			if toCloseConn, ok := toClose.Get(); ok {
				require.True(t, toCloseConn.Closed())
			}
		}
	}
}

func TestPeerManager_Subscribe(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	maxConns := 60
	m := makePeerManager(makeNodeID(rng), &RouterOptions{
		MaxConnected: utils.Some(maxConns),
	})
	t.Log("initialize with some connections")
	for range 5 {
		require.NoError(t, m.Connected(makeConn(rng, utils.GenBool(rng))))
	}
	t.Log("subscribe with preexisting connections")
	recv := m.Subscribe()
	got := map[types.NodeID]bool{}
	for range 100 {
		// Modify connections.
		for range 10 {
			conns := m.Conns()
			if conns.Len() == 0 || (conns.Len() < maxConns && utils.GenBool(rng)) {
				require.NoError(t, m.Connected(makeConn(rng, utils.GenBool(rng))))
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

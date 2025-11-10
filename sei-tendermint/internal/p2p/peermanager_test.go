package p2p

import (
	"fmt"
	"context"
	"testing"
	"time"
	"math/rand"
	"sync/atomic"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/crypto/ed25519"

	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/types"
)

var selfID = types.NodeIDFromPubKey(ed25519.GenPrivKeyFromSecret([]byte("selfID")).PubKey())

func makeKey(rng *rand.Rand) ed25519.PrivKey {
	return ed25519.GenPrivKeyFromSecret(utils.GenBytes(rng, 32))
}

func makeNodeID(rng *rand.Rand) types.NodeID {
	return types.NodeIDFromPubKey(makeKey(rng).PubKey())
}

func makeAddrFor(rng *rand.Rand, id types.NodeID) NodeAddress {
	return NodeAddress{
		NodeID:   id,
		Hostname: fmt.Sprintf("%s.example.com",utils.GenString(rng,10)),
		Port:     uint16(rng.Int()),
	}
}

func makeAddr(rng *rand.Rand) NodeAddress {
	return makeAddrFor(rng,makeNodeID(rng))
}

func (m *PeerManager) Addresses(id types.NodeID) []NodeAddress {
	var addrs []NodeAddress
	for inner := range m.inner.Lock() {
		peerAddrs := inner.addrs
		if inner.isPersistent(id) {
			peerAddrs = inner.persistentAddrs
		}
		if pa,ok := peerAddrs[id]; ok {
			for addr := range pa.addrs {
				addrs = append(addrs,addr)
			}
		}
	}
	return addrs
}

func TestRouterOptions(t *testing.T) {
	rng := utils.TestRng()

	makeOpts := func(rng *rand.Rand) RouterOptions {
		addrs := utils.Slice(makeAddr(rng),makeAddr(rng),makeAddr(rng))
		addrs2 := utils.Slice(makeAddr(rng))
		ids := utils.Slice(addrs[0].NodeID,addrs[2].NodeID)
		return RouterOptions {
			PersistentPeers: addrs,
			BootstrapPeers: addrs2,
			BlockSyncPeers: ids,
			UnconditionalPeers: ids,
			PrivatePeers: ids,
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
		"empty": {RouterOptions{}, true},
		"valid": {optsOk, true},
		"bad PersistentPeers": {optsBadPersistent, false},
		"bad BootstrapPeers": {optsBadBootstrap, false},
		"bad BlockSyncPeers": {optsBadBlockSync, false},
		"bad UnconditionalPeers": {optsBadUnconditional, false},
		"bad PrivatePeers": {optsBadPrivate, false},
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
	for range 6 { ids = append(ids, makeNodeID(rng)) }
	addrs := map[types.NodeID][]NodeAddress{}
	for _,id := range ids {
		for range rng.Intn(3)+2 {
			addrs[id] = append(addrs[id],makeAddrFor(rng,id))
		}
	}

	t.Log("Collect all persistent peers' addrs.")
	var persistentAddrs []NodeAddress
	for _,id := range ids[:2] {
		persistentAddrs = append(persistentAddrs, addrs[id]...)
	}
	t.Log("Collect some other peers' addrs.")
	var bootstrapAddrs []NodeAddress
	for _,id := range ids[2:] {
		bootstrapAddrs = append(bootstrapAddrs, addrs[id][:2]...)
	}

	t.Log("Create peer manager.")
	maxPeers := 10
	m := newPeerManager(selfID, &RouterOptions{
		BootstrapPeers: bootstrapAddrs,
		PersistentPeers: persistentAddrs,
		UnconditionalPeers: utils.Slice(ids[2]),
		BlockSyncPeers: utils.Slice(ids[3]),
		PrivatePeers: utils.Slice(ids[4]),
		MaxPeers: utils.Some(maxPeers),
	})

	t.Log("Check that all expected addrs are present.")
	for _,id := range ids[:2] {
		require.ElementsMatch(t, addrs[id], m.Addresses(id))
	}
	for _,id := range ids[2:] {
		require.ElementsMatch(t, addrs[id][:2], m.Addresses(id))
	}

	t.Log("Add all addresses at once.")
	var allAddrs []NodeAddress
	for _,id := range ids {
		allAddrs = append(allAddrs,addrs[id]...)
	}
	require.NoError(t, m.AddAddrs(allAddrs))

	t.Log("Check that all expected addrs are present.")
	for _,id := range ids {
		require.ElementsMatch(t, addrs[id], m.Addresses(id))
	}

	t.Log("Check that adding new persistent peer address is ignored.")
	require.NoError(t, m.AddAddrs(utils.Slice(makeAddrFor(rng, ids[0]))))
	require.ElementsMatch(t, addrs[ids[0]], m.Addresses(ids[0]))

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
		ids = append(ids,addr.NodeID)
		addrs[addr.NodeID] = utils.Slice(addr)
		newAddrs = append(newAddrs,addr)
	}
	require.NoError(t, m.AddAddrs(newAddrs))
	expectedIDs := maxPeers+2 // There are 2 persistent peers.
	for _,id := range ids[:expectedIDs] {
		expectedAddrs := min(len(addrs[id]),maxAddrsPerPeer)
		require.ElementsMatch(t, addrs[id][:expectedAddrs], m.Addresses(id))
	}
	for _,id := range ids[expectedIDs:] {
		require.ElementsMatch(t, nil, m.Addresses(id))
	}

	t.Log("Check that failed addresses are replaceable")
	m.DialFailed(addrs[idWithManyAddrs][1]) // we fail 1 arbitrary adderess
	newAddrs = addrs[idWithManyAddrs][maxAddrsPerPeer:]
	require.NoError(t, m.AddAddrs(newAddrs)) // we try to add some addrs
	want := append([]NodeAddress(nil),addrs[idWithManyAddrs][:maxAddrsPerPeer]...)
	want[1] = newAddrs[0] // the first one newly added should replace the failed one.
	require.ElementsMatch(t, want, m.Addresses(idWithManyAddrs))

	t.Log("Check that failed peers are replaceable")
	for _,addr := range addrs[ids[4]] {
		m.DialFailed(addr)
	}
	newPeer := makeAddr(rng)
	require.NoError(t, m.AddAddrs(utils.Slice(newPeer)))
	require.ElementsMatch(t, nil, m.Addresses(ids[4]))
	require.ElementsMatch(t, utils.Slice(newPeer), m.Addresses(newPeer.NodeID))
}

func TestPeerManager_DuplicateDials(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	maxDials := 3
	m := newPeerManager(makeNodeID(rng), &RouterOptions {
		MaxConcurrentDials: utils.Some(maxDials),
	})
	var addrs []NodeAddress
	addrMap := map[NodeAddress]bool{}
	dialing := utils.NewMutex(map[types.NodeID]bool{})
	for range 10 {
		addr := makeAddr(rng)
		addrs = append(addrs, addr)
		addrMap[addr] = true
	}
	require.NoError(t, m.AddAddrs(addrs))
	var total atomic.Int64
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for range 20 {
			addr,err := m.StartDial(ctx, false) // dial non-persistent peer
			if err!=nil {
				return fmt.Errorf("StartDial(): %w",err)
			}
			if !addrMap[addr] {
				return fmt.Errorf("unexpected addr")
			}
			for dialing := range dialing.Lock() {
				if dialing[addr.NodeID] {
					return fmt.Errorf("dialing same peer on 2 addresses")
				}
				dialing[addr.NodeID] = true
			}
			if got:=total.Add(1); got>int64(maxDials) {
				return fmt.Errorf("dials limit exceeded: %v",got)
			}
			s.Spawn(func() error {
				if err:=utils.Sleep(ctx, 100*time.Millisecond); err!=nil {
					return err
				}
				total.Add(-1)
				m.DialFailed(addr)
				return nil
			})
		}
		return nil
	})
	if err!=nil {
		t.Fatal(err)
	}
}

func TestPeerManager_ConcurrentDials(t *testing.T) {
	ctx := t.Context()
	for _,tc := range []struct{ peers, maxDials, rounds int } {
		{peers: 10, maxDials: 3, rounds: 20}, // dialing limited by MaxConcurrentDials
		{peers: 3, maxDials: 10, rounds: 20}, // dialing limited by available peer addrs
	} {
		t.Run(fmt.Sprintf("peers=%v maxDials=%v",tc.peers,tc.maxDials), func(t *testing.T) {
			rng := utils.TestRng()
			m := newPeerManager(makeNodeID(rng), &RouterOptions {
				MaxConcurrentDials: utils.Some(tc.maxDials),
			})
			addrsMap := map[NodeAddress]bool{}
			var addrs []NodeAddress
			for range tc.peers {
				addr := makeAddr(rng)
				addrs = append(addrs, addr)
				addrsMap[addr] = true
			}
			require.NoError(t, m.AddAddrs(addrs))
			dialing := utils.NewMutex(map[types.NodeID]bool{})
			err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
				for range tc.rounds {
					addr,err := m.StartDial(ctx, false) // dial non-persistent peer
					if err!=nil {
						return fmt.Errorf("StartDial(): %w",err)
					}
					if !addrsMap[addr] {
						return fmt.Errorf("unexpected addr %v",addr)
					}
					for dialing := range dialing.Lock() {
						if got:=len(dialing); got>tc.maxDials {
							return fmt.Errorf("dials limit exceeded: %v",got)
						}
						if dialing[addr.NodeID] {
							return fmt.Errorf("duplicate concurrent dials for %v",addr.NodeID)
						}
						dialing[addr.NodeID] = true
					}
					s.Spawn(func() error {
						if err:=utils.Sleep(ctx, 50*time.Millisecond); err!=nil {
							return err
						}
						for dialing := range dialing.Lock() {
							delete(dialing,addr.NodeID)
						}
						m.DialFailed(addr)
						return nil
					})
				}
				return nil
			})
			if err!=nil {
				t.Fatal(err)
			}
		})
	}
}

// Test checking that all the provided addresses are eventually dialed.
func TestPeerManager_DialRoundRobin(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	m := newPeerManager(makeNodeID(rng), &RouterOptions {})
	addrsMap := map[NodeAddress]bool{}
	var addrs []NodeAddress
	for range 10 {
		id := makeNodeID(rng)
		for range rng.Intn(5)+1 {
			addr := makeAddrFor(rng,id)
			addrsMap[addr] = true
			addrs = append(addrs, addr)
		}
	}
	require.NoError(t, m.AddAddrs(addrs))
	for len(addrsMap)>0 {
		addr,err := m.StartDial(ctx, false) // dial non-persistent peer
		require.NoError(t, err)
		delete(addrsMap,addr)
		m.DialFailed(addr)
	}
}


/*
func TestPeerManager_DialNext(t *testing.T) {
	ctx := t.Context()

	a := makeAddr()"a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{}, NopMetrics())
	require.NoError(t, err)

	// Add an address. DialNext should return it.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	address, err := peerManager.DialNext(ctx)
	require.NoError(t, err)
	require.Equal(t, a, address)

	// Since there are no more undialed peers, the next call should block
	// until it times out.
	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, err = peerManager.DialNext(timeoutCtx)
	require.Error(t, err)
	require.Equal(t, context.DeadlineExceeded, err)
}

func TestPeerManager_DialNext_Retry(t *testing.T) {
	ctx := t.Context()

	a := makeAddr()

	options := RouterOptions{
	}
	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), options, NopMetrics())
	require.NoError(t, err)

	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)

	for i := 0; i < 3; i++ {
		start := time.Now()
		dial, err := peerManager.DialNext(ctx)
		require.NoError(t, err)
		require.Equal(t, a, dial)
		elapsed := time.Since(start).Round(time.Millisecond)
		if i > 0 {
			require.GreaterOrEqual(t, elapsed, time.Duration(math.Pow(2, float64(i)))*options.MinRetryTime)
		}
		require.NoError(t, peerManager.DialFailed(ctx, a))
	}
}

func TestPeerManager_Dial(t *testing.T) {
	// Wake Persistent/non-persistent
	// OnAddedAddr - addr avail
	// OnDialFailed - dial slot avail, addr avail
	// OnConnect - dial slot avail
	// OnDisconnect - should reconnect

	// Respect MaxConnected/MaxDial
	// No conn/Dial duplicates
	// another test: interactions with inbound conns.
}

func TestPeerManager_DialFailed(t *testing.T) {
	// DialFailed is tested through other tests, we'll just check a few basic
	// things here, e.g. reporting unknown addresses.
	a := makeAddr()"a")
	b := makeAddr()"b")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{}, NopMetrics())
	require.NoError(t, err)

	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)

	ctx := t.Context()

	// Dialing and then calling DialFailed with a different address (same
	// NodeID) should unmark as dialing and allow us to dial the other address
	// again, but not register the failed address.
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, a, dial)
	require.NoError(t, peerManager.DialFailed(ctx, NodeAddress{
		NodeID: a.NodeID, Hostname: "localhost"}))
	require.Equal(t, []NodeAddress{a}, peerManager.Addresses(a.NodeID))

	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, a, dial)

	// Calling DialFailed on same address twice should be fine.
	require.NoError(t, peerManager.DialFailed(ctx, a))
	require.NoError(t, peerManager.DialFailed(ctx, a))

	// DialFailed on an unknown peer shouldn't error or add it.
	require.NoError(t, peerManager.DialFailed(ctx, b))
	require.Equal(t, []types.NodeID{a.NodeID}, peerManager.Peers())
}

func TestPeerManager_DialFailed_UnreservePeer(t *testing.T) {
	ctx := t.Context()

	a := makeAddr()"a")
	b := makeAddr()"b")
	c := makeAddr()"c")

	logger, _ := log.NewDefaultLogger("plain", "debug")
	peerManager, err := NewPeerManager(logger, selfID, dbm.NewMemDB(), RouterOptions{
		PeerScores: map[types.NodeID]PeerScore{
			a.NodeID: DefaultMutableScore - 1, // Set lower score for a to make it upgradeable
			b.NodeID: DefaultMutableScore + 1, // Higher score for b to attempt upgrade
			c.NodeID: DefaultMutableScore + 2, // Higher score for c to ensure it's selected after b fails
		},
		MaxConnected:        1,
		MaxConnectedUpgrade: 2,
	}, NopMetrics())
	require.NoError(t, err)

	t.Logf("Add and connect to peer a (lower scored)")
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, a, dial)
	require.NoError(t, peerManager.Dialed(a))

	t.Logf("Add both higher scored peers b and c")
	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	added, err = peerManager.Add(c) // Add c before attempting upgrade
	require.NoError(t, err)
	require.True(t, added)

	// Attempt to dial c for upgrade (higher score)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, c, dial)

	// When c's dial fails, the upgrade slot should be freed
	// allowing b to attempt upgrade of the same peer (a)
	require.NoError(t, peerManager.DialFailed(ctx, c))
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, b, dial)
}

func TestPeerManager_Dialed_Connected(t *testing.T) {
	a := makeAddr()"a")
	b := makeAddr()"b")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{}, NopMetrics())
	require.NoError(t, err)

	// Marking a as dialed twice should error.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, a, dial)

	require.NoError(t, peerManager.Dialed(a))
	require.Error(t, peerManager.Dialed(a))

	// Accepting a connection from b and then trying to mark it as dialed should fail.
	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, b, dial)

	require.NoError(t, peerManager.Accepted(b.NodeID))
	require.Error(t, peerManager.Dialed(b))
}

func TestPeerManager_Dialed_Self(t *testing.T) {
	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{}, NopMetrics())
	require.NoError(t, err)

	// Dialing self should error.
	added, err := peerManager.Add(NodeAddress{NodeID: selfID, Hostname: "a.com", Port: 1234})
	require.Nil(t, err)
	require.False(t, added)
}

func TestPeerManager_Dialed_MaxConnected(t *testing.T) {
	a := makeAddr()"a")
	b := makeAddr()"b")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{
		MaxConnected: 1,
	}, NopMetrics())
	require.NoError(t, err)

	// Start to dial a.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, a, dial)

	// Marking b as dialed in the meanwhile (even without TryDialNext)
	// should be fine.
	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Dialed(b))

	// Completing the dial for a should now error.
	require.Error(t, peerManager.Dialed(a))
}

func TestPeerManager_Dialed_MaxConnectedUpgrade(t *testing.T) {
	a := makeAddr()"a")
	b := makeAddr()"b")
	c := makeAddr()"c")
	d := makeAddr()"d")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{
		MaxConnected:        2,
		MaxConnectedUpgrade: 1,
		PeerScores: map[types.NodeID]PeerScore{
			a.NodeID: DefaultMutableScore - 1, // Lower score for a
			b.NodeID: DefaultMutableScore - 1, // Lower score for b
			c.NodeID: DefaultMutableScore + 1, // Higher score for c to upgrade
			d.NodeID: DefaultMutableScore + 1, // Higher score for d to upgrade
		},
	}, NopMetrics())
	require.NoError(t, err)

	// Connect to lower scored peers a and b
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Dialed(a))

	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Dialed(b))

	// Add both higher scored peers c and d
	added, err = peerManager.Add(c)
	require.NoError(t, err)
	require.True(t, added)
	added, err = peerManager.Add(d)
	require.NoError(t, err)
	require.True(t, added)

	// Start upgrade with c
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	if dial != c && dial != d {
		t.Fatalf("dial = %s, expected %s or %s", dial, c, d)
	}
	require.NoError(t, peerManager.Dialed(dial))

	// Try to dial d - should fail since we're at upgrade capacity
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Zero(t, dial)
	require.Error(t, peerManager.Dialed(d))
}

func TestPeerManager_Dialed_Unknown(t *testing.T) {
	a := makeAddr()"a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{}, NopMetrics())
	require.NoError(t, err)

	// Marking an unknown node as dialed should error.
	require.Error(t, peerManager.Dialed(a))
}

func TestPeerManager_Dialed_Upgrade(t *testing.T) {
	a := makeAddr()"a")
	b := makeAddr()"b")
	c := makeAddr()"c")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{
		MaxConnected:        1,
		MaxConnectedUpgrade: 2,
		PeerScores:          map[types.NodeID]PeerScore{b.NodeID: DefaultMutableScore + 1, c.NodeID: DefaultMutableScore + 1},
	}, NopMetrics())
	require.NoError(t, err)

	// Dialing a is fine.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Dialed(a))

	// Upgrading it with b should work, since b has a higher score.
	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, b, dial)
	require.NoError(t, peerManager.Dialed(b))

	// a hasn't been evicted yet, but c shouldn't be allowed to upgrade anyway
	// since it's about to be evicted.
	added, err = peerManager.Add(c)
	require.NoError(t, err)
	require.True(t, added)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Empty(t, dial)

	// a should now be evicted.
	evict, err := peerManager.TryEvictNext()
	require.NoError(t, err)
	if ev, ok := evict.Get(); !ok || ev.ID != a.NodeID {
		t.Fatalf("evict = %v, expected %s", evict, a.NodeID)
	}
}

func TestPeerManager_Dialed_UpgradeEvenLower(t *testing.T) {
	ctx := t.Context()

	a := makeAddr()"a")
	b := makeAddr()"b")
	c := makeAddr()"c")
	d := makeAddr()"d")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{
		MaxConnected:        2,
		MaxConnectedUpgrade: 1,
		PeerScores: map[types.NodeID]PeerScore{
			a.NodeID: 3,
			b.NodeID: 2,
			c.NodeID: 10,
			d.NodeID: 1,
		},
	}, NopMetrics())
	require.NoError(t, err)

	// Connect to a and b.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Dialed(a))

	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Dialed(b))

	// Start an upgrade with c, which should pick b to upgrade (since it
	// has score 2).
	added, err = peerManager.Add(c)
	require.NoError(t, err)
	require.True(t, added)
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, c, dial)

	// In the meanwhile, a disconnects and d connects. d is even lower-scored
	// than b (1 vs 2), which is currently being upgraded.
	peerManager.Disconnected(ctx, a.NodeID)
	added, err = peerManager.Add(d)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(d.NodeID))

	// Once c completes the upgrade of b, it should instead evict d,
	// since it has en even lower score.
	require.NoError(t, peerManager.Dialed(c))
	evict, err := peerManager.TryEvictNext()
	require.NoError(t, err)
	if ev, ok := evict.Get(); !ok || ev.ID != d.NodeID {
		t.Fatalf("evict = %v, expected %s", evict, d.NodeID)
	}
}

func TestPeerManager_Dialed_UpgradeNoEvict(t *testing.T) {
	ctx := t.Context()

	a := makeAddr()"a")
	b := makeAddr()"b")
	c := makeAddr()"c")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{
		MaxConnected:        2,
		MaxConnectedUpgrade: 1,
		PeerScores: map[types.NodeID]PeerScore{
			a.NodeID: 1,
			b.NodeID: 2,
			c.NodeID: 3,
		},
	}, NopMetrics())
	require.NoError(t, err)

	// Connect to a and b.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Dialed(a))

	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Dialed(b))

	// Start an upgrade with c, which should pick a to upgrade.
	added, err = peerManager.Add(c)
	require.NoError(t, err)
	require.True(t, added)
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, c, dial)

	// In the meanwhile, b disconnects.
	peerManager.Disconnected(ctx, b.NodeID)

	// Once c completes the upgrade of b, there is no longer a need to
	// evict anything since we're at capacity.
	// since it has en even lower score.
	require.NoError(t, peerManager.Dialed(c))
	evict, err := peerManager.TryEvictNext()
	require.NoError(t, err)
	require.Zero(t, evict)
}

func TestPeerManager_Accepted(t *testing.T) {
	a := makeAddr()"a")
	b := makeAddr()"b")
	c := makeAddr()"c")
	d := makeAddr()"d")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{}, NopMetrics())
	require.NoError(t, err)

	// Accepting a connection from self should error.
	require.Error(t, peerManager.Accepted(selfID))

	// Accepting a connection from a known peer should work.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(a.NodeID))

	// Accepting a connection from an already accepted peer should error.
	require.Error(t, peerManager.Accepted(a.NodeID))

	// Accepting a connection from an unknown peer should work and register it.
	require.NoError(t, peerManager.Accepted(b.NodeID))
	require.ElementsMatch(t, []types.NodeID{a.NodeID, b.NodeID}, peerManager.Peers())

	// Accepting a connection from a peer that's being dialed should work, and
	// should cause the dial to fail.
	added, err = peerManager.Add(c)
	require.NoError(t, err)
	require.True(t, added)
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, c, dial)
	require.NoError(t, peerManager.Accepted(c.NodeID))
	require.Error(t, peerManager.Dialed(c))

	// Accepting a connection from a peer that's been dialed should fail.
	added, err = peerManager.Add(d)
	require.NoError(t, err)
	require.True(t, added)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, d, dial)
	require.NoError(t, peerManager.Dialed(d))
	require.Error(t, peerManager.Accepted(d.NodeID))
}

func TestPeerManager_Accepted_MaxConnected(t *testing.T) {
	a := makeAddr()"a")
	b := makeAddr()"b")
	c := makeAddr()"c")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{
		MaxConnected: 2,
	}, NopMetrics())
	require.NoError(t, err)

	// Connect to a and b.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Dialed(a))

	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(b.NodeID))

	// Accepting c should now fail.
	added, err = peerManager.Add(c)
	require.NoError(t, err)
	require.True(t, added)
	require.Error(t, peerManager.Accepted(c.NodeID))
}

func TestPeerManager_Accepted_MaxConnectedUpgrade(t *testing.T) {
	a := makeAddr()"a")
	b := makeAddr()"b")
	c := makeAddr()"c")
	d := makeAddr()"d")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{
		PeerScores: map[types.NodeID]PeerScore{
			c.NodeID: DefaultMutableScore + 1,
			d.NodeID: DefaultMutableScore + 2,
		},
		MaxConnected:        1,
		MaxConnectedUpgrade: 1,
	}, NopMetrics())
	require.NoError(t, err)

	// Dial a.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Dialed(a))

	// Accepting b should fail, since it's not an upgrade over a.
	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	require.Error(t, peerManager.Accepted(b.NodeID))

	// Accepting c should work, since it upgrades a.
	added, err = peerManager.Add(c)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(c.NodeID))

	// a still hasn't been evicted, so accepting b should still fail.
	_, err = peerManager.Add(b)
	require.NoError(t, err)
	require.Error(t, peerManager.Accepted(b.NodeID))

	// Also, accepting d should fail, since all upgrade slots are full.
	added, err = peerManager.Add(d)
	require.NoError(t, err)
	require.True(t, added)
	require.Error(t, peerManager.Accepted(d.NodeID))
}

func TestPeerManager_Accepted_Upgrade(t *testing.T) {
	ctx := t.Context()

	a := makeAddr()"a")
	b := makeAddr()"b")
	c := makeAddr()"c")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{
		PeerScores: map[types.NodeID]PeerScore{
			b.NodeID: DefaultMutableScore + 1,
			c.NodeID: DefaultMutableScore + 1,
		},
		MaxConnected:        1,
		MaxConnectedUpgrade: 2,
	}, NopMetrics())
	require.NoError(t, err)

	// Accept a.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(a.NodeID))

	// Accepting b should work, since it upgrades a.
	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(b.NodeID))

	// c cannot get accepted, since a has been upgraded by b.
	added, err = peerManager.Add(c)
	require.NoError(t, err)
	require.True(t, added)
	require.Error(t, peerManager.Accepted(c.NodeID))

	// This should cause a to get evicted.
	evict, err := peerManager.TryEvictNext()
	require.NoError(t, err)
	if ev, ok := evict.Get(); !ok || ev.ID != a.NodeID {
		t.Fatalf("evict = %v, expected %s", evict, a.NodeID)
	}
	peerManager.Disconnected(ctx, a.NodeID)

	// c still cannot get accepted, since it's not scored above b.
	require.Error(t, peerManager.Accepted(c.NodeID))
}

func TestPeerManager_Accepted_UpgradeDialing(t *testing.T) {
	a := makeAddr()"a")
	b := makeAddr()"b")
	c := makeAddr()"c")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{
		PeerScores: map[types.NodeID]PeerScore{
			b.NodeID: DefaultMutableScore + 1,
			c.NodeID: DefaultMutableScore + 1,
		},
		MaxConnected:        1,
		MaxConnectedUpgrade: 2,
	}, NopMetrics())
	require.NoError(t, err)

	// Accept a.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(a.NodeID))

	// Start dial upgrade from a to b.
	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, b, dial)

	// a has already been claimed as an upgrade of a, so accepting
	// c should fail since there's noone else to upgrade.
	added, err = peerManager.Add(c)
	require.NoError(t, err)
	require.True(t, added)
	require.Error(t, peerManager.Accepted(c.NodeID))

	// However, if b connects to us while we're also trying to upgrade to it via
	// dialing, then we accept the incoming connection as an upgrade.
	require.NoError(t, peerManager.Accepted(b.NodeID))

	// This should cause a to get evicted, and the dial upgrade to fail.
	evict, err := peerManager.TryEvictNext()
	require.NoError(t, err)
	if ev, ok := evict.Get(); !ok || ev.ID != a.NodeID {
		t.Fatalf("evict = %v, expected %s", evict, a.NodeID)
	}
	require.Error(t, peerManager.Dialed(b))
}

func TestPeerManager_Ready(t *testing.T) {
	a := makeAddr()"a")
	b := makeAddr()"b")

	ctx := t.Context()

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{}, NopMetrics())
	require.NoError(t, err)

	sub := peerManager.Subscribe(ctx)

	// Connecting to a should still have it as status down.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(a.NodeID))
	require.Equal(t, PeerStatusDown, peerManager.Status(a.NodeID))

	// Marking a as ready should transition it to PeerStatusUp and send an update.
	peerManager.Ready(ctx, a.NodeID, nil)
	require.Equal(t, PeerStatusUp, peerManager.Status(a.NodeID))
	require.Equal(t, PeerUpdate{
		NodeID: a.NodeID,
		Status: PeerStatusUp,
	}, <-sub.Updates())

	// Marking an unconnected peer as ready should do nothing.
	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	require.Equal(t, PeerStatusDown, peerManager.Status(b.NodeID))
	peerManager.Ready(ctx, b.NodeID, nil)
	require.Equal(t, PeerStatusDown, peerManager.Status(b.NodeID))
	require.Empty(t, sub.Updates())
}

func TestPeerManager_Ready_Channels(t *testing.T) {
	ctx := t.Context()

	pm, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{}, NopMetrics())
	require.NoError(t, err)

	sub := pm.Subscribe(ctx)

	a := makeAddr()"a")
	added, err := pm.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, pm.Accepted(a.NodeID))

	pm.Ready(ctx, a.NodeID, ChannelIDSet{42: struct{}{}})
	require.NotEmpty(t, sub.Updates())
	update := <-sub.Updates()
	assert.Equal(t, a.NodeID, update.NodeID)
	require.True(t, update.Channels.Contains(42))
	require.False(t, update.Channels.Contains(48))
}

// See TryEvictNext for most tests, this just tests blocking behavior.
func TestPeerManager_EvictNext(t *testing.T) {
	ctx := t.Context()

	a := makeAddr()"a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{}, NopMetrics())
	require.NoError(t, err)

	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(a.NodeID))
	peerManager.Ready(ctx, a.NodeID, nil)

	// Since there are no peers to evict, EvictNext should block until timeout.
	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, err = peerManager.EvictNext(timeoutCtx)
	require.Error(t, err)
	require.Equal(t, context.DeadlineExceeded, err)

	// Erroring the peer will return it from EvictNext().
	peerManager.Errored(a.NodeID, errors.New("foo"))
	evict, err := peerManager.EvictNext(timeoutCtx)
	require.NoError(t, err)
	require.Equal(t, a.NodeID, evict.ID)

	// Since there are no more peers to evict, the next call should block.
	timeoutCtx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, err = peerManager.EvictNext(timeoutCtx)
	require.Error(t, err)
	require.Equal(t, context.DeadlineExceeded, err)
}

func TestPeerManager_EvictNext_WakeOnError(t *testing.T) {
	ctx := t.Context()

	a := makeAddr()"a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{}, NopMetrics())
	require.NoError(t, err)

	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(a.NodeID))
	peerManager.Ready(ctx, a.NodeID, nil)

	// Spawn a goroutine to error a peer after a delay.
	go func() {
		time.Sleep(200 * time.Millisecond)
		peerManager.Errored(a.NodeID, errors.New("foo"))
	}()

	// This will block until peer errors above.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	evict, err := peerManager.EvictNext(ctx)
	require.NoError(t, err)
	require.Equal(t, a.NodeID, evict.ID)
}

func TestPeerManager_EvictNext_WakeOnUpgradeDialed(t *testing.T) {
	ctx := t.Context()

	a := makeAddr()"a")
	b := makeAddr()"b")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{
		MaxConnected:        1,
		MaxConnectedUpgrade: 1,
		PeerScores:          map[types.NodeID]PeerScore{b.NodeID: DefaultMutableScore + 1},
	}, NopMetrics())
	require.NoError(t, err)

	// Connect a.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(a.NodeID))
	peerManager.Ready(ctx, a.NodeID, nil)

	// Spawn a goroutine to upgrade to b with a delay.
	go func() {
		time.Sleep(200 * time.Millisecond)
		added, err := peerManager.Add(b)
		require.NoError(t, err)
		require.True(t, added)
		dial, err := peerManager.TryDialNext()
		require.NoError(t, err)
		require.Equal(t, b, dial)
		require.NoError(t, peerManager.Dialed(b))
	}()

	// This will block until peer is upgraded above.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	evict, err := peerManager.EvictNext(ctx)
	require.NoError(t, err)
	require.Equal(t, a.NodeID, evict.ID)
}

func TestPeerManager_EvictNext_WakeOnUpgradeAccepted(t *testing.T) {
	ctx := t.Context()

	a := makeAddr()"a")
	b := makeAddr()"b")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{
		MaxConnected:        1,
		MaxConnectedUpgrade: 1,
		PeerScores:          map[types.NodeID]PeerScore{b.NodeID: DefaultMutableScore + 1},
	}, NopMetrics())
	require.NoError(t, err)

	// Connect a.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(a.NodeID))
	peerManager.Ready(ctx, a.NodeID, nil)

	// Spawn a goroutine to upgrade b with a delay.
	go func() {
		time.Sleep(200 * time.Millisecond)
		require.NoError(t, peerManager.Accepted(b.NodeID))
	}()

	// This will block until peer is upgraded above.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	evict, err := peerManager.EvictNext(ctx)
	require.NoError(t, err)
	require.Equal(t, a.NodeID, evict.ID)
}
func TestPeerManager_TryEvictNext(t *testing.T) {
	ctx := t.Context()

	a := NodeAddress{
		NodeID:   types.NodeID(strings.Repeat("a", 40)),
		Hostname: "a.com",
		Port:     26657,
	}

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{}, NopMetrics())
	require.NoError(t, err)

	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)

	// Nothing is evicted with no peers connected.
	evict, err := peerManager.TryEvictNext()
	require.NoError(t, err)
	require.Zero(t, evict)

	// Connecting to a won't evict anything either.
	require.NoError(t, peerManager.Accepted(a.NodeID))
	peerManager.Ready(ctx, a.NodeID, nil)

	// But if a errors it should be evicted.
	peerManager.Errored(a.NodeID, errors.New("foo"))
	evict, err = peerManager.TryEvictNext()
	require.NoError(t, err)
	if ev, ok := evict.Get(); !ok || ev.ID != a.NodeID {
		t.Fatalf("evict = %v, expected %s", evict, a.NodeID)
	}

	// While a is being evicted (before disconnect), it shouldn't get evicted again.
	evict, err = peerManager.TryEvictNext()
	require.NoError(t, err)
	require.Zero(t, evict)

	peerManager.Errored(a.NodeID, errors.New("foo"))
	evict, err = peerManager.TryEvictNext()
	require.NoError(t, err)
	require.Zero(t, evict)
}

func TestPeerManager_Disconnected(t *testing.T) {
	a := makeAddr()"a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{}, NopMetrics())
	require.NoError(t, err)

	ctx := t.Context()

	sub := peerManager.Subscribe(ctx)

	// Disconnecting an unknown peer does nothing.
	peerManager.Disconnected(ctx, a.NodeID)
	require.Empty(t, peerManager.Peers())
	require.Empty(t, sub.Updates())

	// Disconnecting an accepted non-ready peer does not send a status update.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(a.NodeID))
	peerManager.Disconnected(ctx, a.NodeID)
	require.Empty(t, sub.Updates())

	// Disconnecting a ready peer sends a status update.
	_, err = peerManager.Add(a)
	require.NoError(t, err)
	require.NoError(t, peerManager.Accepted(a.NodeID))
	peerManager.Ready(ctx, a.NodeID, nil)
	require.Equal(t, PeerStatusUp, peerManager.Status(a.NodeID))
	require.NotEmpty(t, sub.Updates())
	require.Equal(t, PeerUpdate{
		NodeID: a.NodeID,
		Status: PeerStatusUp,
	}, <-sub.Updates())

	peerManager.Disconnected(ctx, a.NodeID)
	require.Equal(t, PeerStatusDown, peerManager.Status(a.NodeID))
	require.NotEmpty(t, sub.Updates())
	require.Equal(t, PeerUpdate{
		NodeID: a.NodeID,
		Status: PeerStatusDown,
	}, <-sub.Updates())

	// Disconnecting a dialing peer does not unmark it as dialing, to avoid
	// dialing it multiple times in parallel.
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, a, dial)

	peerManager.Disconnected(ctx, a.NodeID)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Zero(t, dial)
}

func TestPeerManager_Evict(t *testing.T) {
}

func TestPeerManager_Subscribe(t *testing.T) {
	ctx := t.Context()

	a := makeAddr()"a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{}, NopMetrics())
	require.NoError(t, err)

	// This tests all subscription events for full peer lifecycles.
	sub := peerManager.Subscribe(ctx)

	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.Empty(t, sub.Updates())

	// Inbound connection.
	require.NoError(t, peerManager.Accepted(a.NodeID))
	require.Empty(t, sub.Updates())

	peerManager.Ready(ctx, a.NodeID, nil)
	require.NotEmpty(t, sub.Updates())
	require.Equal(t, PeerUpdate{NodeID: a.NodeID, Status: PeerStatusUp}, <-sub.Updates())

	peerManager.Disconnected(ctx, a.NodeID)
	require.NotEmpty(t, sub.Updates())
	require.Equal(t, PeerUpdate{NodeID: a.NodeID, Status: PeerStatusDown}, <-sub.Updates())

	// Outbound connection with peer error and eviction.
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, a, dial)
	require.Empty(t, sub.Updates())

	require.NoError(t, peerManager.Dialed(a))
	require.Empty(t, sub.Updates())

	peerManager.Ready(ctx, a.NodeID, nil)
	require.NotEmpty(t, sub.Updates())
	require.Equal(t, PeerUpdate{NodeID: a.NodeID, Status: PeerStatusUp}, <-sub.Updates())

	peerManager.Errored(a.NodeID, errors.New("foo"))
	require.Empty(t, sub.Updates())

	evict, err := peerManager.TryEvictNext()
	require.NoError(t, err)
	if ev, ok := evict.Get(); !ok || ev.ID != a.NodeID {
		t.Fatalf("evict = %v, expected %s", evict, a.NodeID)
	}

	peerManager.Disconnected(ctx, a.NodeID)
	require.NotEmpty(t, sub.Updates())
	require.Equal(t, PeerUpdate{NodeID: a.NodeID, Status: PeerStatusDown}, <-sub.Updates())

	// Outbound connection with dial failure.
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, a, dial)
	require.Empty(t, sub.Updates())

	require.NoError(t, peerManager.DialFailed(ctx, a))
	require.Empty(t, sub.Updates())
}*/

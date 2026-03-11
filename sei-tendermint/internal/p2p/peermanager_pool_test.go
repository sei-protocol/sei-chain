package p2p

import (
	"sort"
	"maps"
	"iter"
	"slices"
	"testing"
	"cmp"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func inPoolAll(types.NodeID) bool { return true }

func opt[T any](v T, ok bool) utils.Option[T] {
	if ok {
		return utils.Some(v)
	}
	return utils.None[T]()
}

func minBy[T any, I cmp.Ordered](vals iter.Seq[T], by func(T) I) utils.Option[T] {
	var m utils.Option[T]
	for v := range vals {
		if x,ok := m.Get(); !ok || by(v) < by(x) { 
			m = utils.Some(v)
		}
	}
	return m
}

// Test checking that TryStartDial respects MaxOut limit.
// - no more dials than len(out)+len(dialing)
// - permit is required for upgrade.
// - only upgrade dial is allowed when MaxOut is saturated.
// - no parallel upgrades allowed even if permit is provided.
func TestPoolManager_TryStartDial_MaxOut(t *testing.T) {
	rng := utils.TestRng()
	const maxIn = 0
	const maxOut = 5 
	fixedAddrs := make([]NodeAddress,maxOut)
	want := map[types.NodeID][]NodeAddress{}
	for i := range fixedAddrs {
		addr := makeAddr(rng)
		fixedAddrs[i] = addr 
		want[addr.NodeID] = utils.Slice(addr)
	}
	pool := newPoolManager(&poolConfig {MaxIn: 0, MaxOut: maxOut, FixedAddrs: fixedAddrs, InPool: inPoolAll})
	got := map[types.NodeID][]NodeAddress{}
	t.Log("initial maxOut dials should succeed")
	for range maxOut {
		addrs := opt(pool.TryStartDial()).OrPanic("")
		got[addrs[0].NodeID] = addrs
	}
	require.NoError(t,utils.TestDiff(want,got))
	t.Log("successful connects do not free dialing slots")
	for id := range got {
		// no successful dial.
		require.False(t,opt(pool.TryStartDial()).IsPresent())
		// connects should not cause evictions.
		require.False(t,utils.OrPanic1(pool.Connect(connID{id,true})).IsPresent())
	}
	
	t.Log("Find a peer which should be able to upgrade connected peer.")
	lowPriority := minBy(maps.Keys(got),pool.priority).OrPanic("")
	newAddr := makeAddr(rng)
	for pool.priority(newAddr.NodeID) <= pool.priority(lowPriority) {
		newAddr = makeAddr(rng)
	}
	pool.PushPex(utils.Some(newAddr.NodeID), utils.Slice(newAddr))
	require.False(t,opt(pool.TryStartDial()).IsPresent())
	t.Log("successful dial after upgrade permit.")
	pool.PushUpgradePermit()
	require.Equal(t,utils.Some(utils.Slice(newAddr)),opt(pool.TryStartDial()))

	t.Log("find even better peer")
	betterAddr := makeAddr(rng)
	for pool.priority(betterAddr.NodeID) <= pool.priority(newAddr.NodeID) {
		betterAddr = makeAddr(rng)
	}
	pool.PushPex(utils.Some(betterAddr.NodeID), utils.Slice(betterAddr))

	t.Log("even with upgrade permit, start dial should fail until the first upgrade dial finishes")
	pool.PushUpgradePermit()
	require.False(t,opt(pool.TryStartDial()).IsPresent())

	t.Log("finish upgrade successfully, lowest priority peer should be evicted")
	evicted := utils.OrPanic1(pool.Connect(connID{newAddr.NodeID,true})).OrPanic("expected peer to evict")
	require.Equal(t,connID{lowPriority,true},evicted)
	
	t.Log("permit should be cleared")
	require.False(t,opt(pool.TryStartDial()).IsPresent())
	
	t.Log("push the permit again, better peer should be available for dialing")
	pool.PushUpgradePermit()
	require.Equal(t,utils.Some(utils.Slice(betterAddr)),opt(pool.TryStartDial()))
}

// Test checking that pool manager behaves reasonably with MaxOut = 0
func TestPoolManager_MaxOutZero(t *testing.T) {
	rng := utils.TestRng()
	t.Log("populate pool in various ways")
	pool := newPoolManager(&poolConfig{MaxIn:1,MaxOut:0,FixedAddrs:utils.Slice(makeAddr(rng)), InPool:inPoolAll})
	pool.PushPex(utils.Some(makeNodeID(rng)), []NodeAddress{makeAddr(rng)})
	pool.PushPex(utils.None[types.NodeID](), []NodeAddress{makeAddr(rng)})
	pool.PushUpgradePermit()
	t.Log("dialing should fail anyway")
	require.False(t,opt(pool.TryStartDial()).IsPresent())
	t.Log("Connect should work as usual")
	_,err := pool.Connect(connID{makeNodeID(rng),true})
	require.ErrorIs(t,err,errUnexpectedPeer)
	require.False(t,utils.OrPanic1(pool.Connect(connID{makeNodeID(rng),false})).IsPresent())
}

// Test checking that DialFailed WAI:
// - only dialed addresses are accepted
func TestPoolManager_DialFailed(t *testing.T) {
	rng := utils.TestRng()
	addr := makeAddr(rng)
	pool := newPoolManager(&poolConfig{MaxIn:1,MaxOut:1,FixedAddrs:utils.Slice(addr),InPool:inPoolAll})
	t.Log("DialFailed() should error before TryStartDial()")
	require.ErrorIs(t, pool.DialFailed(addr.NodeID), errUnexpectedPeer)
	require.Equal(t,utils.Some(utils.Slice(addr)),opt(pool.TryStartDial()))
	t.Log("DialFailed() should error for peer different than the one returned by TryStartDial()")
	require.ErrorIs(t, pool.DialFailed(makeNodeID(rng)), errUnexpectedPeer)
	t.Log("DialFailed() should succeed for the expected peer")
	require.NoError(t,pool.DialFailed(addr.NodeID))
}

// Test checking that Connected behavior WAI:
// - for outbound only dialed addresses are accepted
// - for inbound the MaxIn is respected.
// - for inbound duplicates are accepted.
// - for outbound upgrade a low prio peer is disconnected and permit is cleared
func TestPoolManager_ConnectDisconnect(t *testing.T) {
	rng := utils.TestRng()
	fixedAddrs := utils.Slice(makeAddr(rng), makeAddr(rng))
	pool := newPoolManager(&poolConfig{MaxIn:1,MaxOut:1,FixedAddrs:fixedAddrs,InPool:inPoolAll})
	
	t.Log("only dialed addresses succeed Connect")
	dialed := opt(pool.TryStartDial()).OrPanic("")[0]
	require.True(t,slices.Contains(fixedAddrs,dialed))
	for _,addr := range fixedAddrs {
		evicted,err := pool.Connect(connID{addr.NodeID,true})
		if addr==dialed {
			require.NoError(t,err)
			require.False(t,evicted.IsPresent())
		} else {
			require.ErrorIs(t,err,errUnexpectedPeer)
		}
	}
	t.Log("duplicate outbound connections are rejected (since they are not dialed)")
	outConn := connID{dialed.NodeID,true}
	_,err := pool.Connect(outConn)
	require.ErrorIs(t,err,errUnexpectedPeer)
	
	t.Log("MaxIn is respected")
	inConn := connID{makeNodeID(rng),false}
	require.False(t,utils.OrPanic1(pool.Connect(inConn)).IsPresent())
	_,err = pool.Connect(connID{makeNodeID(rng),false})
	require.ErrorIs(t,err,errTooManyPeers)

	t.Log("duplicate inbound connection are accepted, replacing the old ones")
	require.Equal(t,utils.Some(inConn),utils.OrPanic1(pool.Connect(inConn)))

	t.Log("only connected addresses succeed Disconnect")
	for _,outbound := range utils.Slice(true,false) {
		require.ErrorIs(t,pool.Disconnect(connID{makeNodeID(rng),outbound}),errUnexpectedPeer)
	}
	for _,conn := range utils.Slice(inConn,outConn) {
		require.NoError(t,pool.Disconnect(conn))
	}
}

// Test checking connected/dialing addresses are not dialed.
// Test checking that disconnected/dial failed addresses are immediately available
// for dialing again in case no other addresses are available.
func TestPoolManager_DialAvailability(t *testing.T) {
	rng := utils.TestRng()
	var fixedAddrs []NodeAddress
	for range 4 { fixedAddrs = append(fixedAddrs, makeAddr(rng)) }
	pool := newPoolManager(&poolConfig{MaxIn:10,MaxOut:10,FixedAddrs:fixedAddrs,InPool:inPoolAll})
	
	t.Log("connect inbound, outbound and dial")
	addr0 := fixedAddrs[0]
	require.False(t,utils.OrPanic1(pool.Connect(connID{addr0.NodeID,false})).IsPresent())
	addr1 := opt(pool.TryStartDial()).OrPanic("")[0]
	addr2 := opt(pool.TryStartDial()).OrPanic("")[0]

	t.Log("none of them should be dialed")
	require.False(t,utils.OrPanic1(pool.Connect(connID{addr1.NodeID,true})).IsPresent())
	busy := utils.Slice(addr0,addr1,addr2)
	require.False(t,slices.Contains(busy,opt(pool.TryStartDial()).OrPanic("")[0]))
	require.False(t,opt(pool.TryStartDial()).IsPresent())

	t.Log("reuse after dial failure")
	require.NoError(t,pool.DialFailed(addr2.NodeID))
	require.Equal(t,utils.Slice(addr2),opt(pool.TryStartDial()).OrPanic(""))
	require.False(t,opt(pool.TryStartDial()).IsPresent())

	t.Log("reuse after inbound disconnect")
	require.NoError(t,pool.Disconnect(connID{addr0.NodeID,false}))
	require.Equal(t,utils.Slice(addr0),opt(pool.TryStartDial()).OrPanic(""))
	require.False(t,opt(pool.TryStartDial()).IsPresent())

	t.Log("reuse after outbound disconnect")
	require.NoError(t,pool.Disconnect(connID{addr1.NodeID,true}))
	require.Equal(t,utils.Slice(addr1),opt(pool.TryStartDial()).OrPanic(""))
	require.False(t,opt(pool.TryStartDial()).IsPresent())
}

// Test checking that TryStartDial does round robin in priority order
// - over all NodeIDs if there is <MaxOut outbound conns
// - over high priority NodeIDs for ==MaxOut outbound conns
// - populate the fixed addrs, bySender and extra (via public api of the poolManager).
func TestPoolManagerRoundRobinAndPriority(t *testing.T) {
	rng := utils.TestRng()
	fixedA, fixedB := makeNodeID(rng), makeNodeID(rng)
	bySenderPeer, extraPeer := makeNodeID(rng), makeNodeID(rng)
	allowed := map[types.NodeID]bool{
		fixedA:       true,
		fixedB:       true,
		bySenderPeer: true,
		extraPeer:    true,
	}
	fixture := newPoolFixture(rng, 1, 2, allowed, fixedA, fixedB)

	senderID := makeNodeID(rng)
	fixture.p.PushPex(utils.Some(senderID), []NodeAddress{fixture.addrs[bySenderPeer]})
	fixture.p.PushPex(utils.None[types.NodeID](), []NodeAddress{fixture.addrs[extraPeer]})

	all := []types.NodeID{fixedA, fixedB, bySenderPeer, extraPeer}
	expected := make([]types.NodeID, len(all))
	copy(expected, all)
	sort.Slice(expected, func(i, j int) bool {
		return fixture.p.priority(expected[i]) > fixture.p.priority(expected[j])
	})

	var got []types.NodeID
	for range expected {
		id := mustStartDialID(t, fixture.p)
		got = append(got, id)
		mustDialFailed(t, fixture.p, id)
	}
	require.Equal(t, expected, got)

	for range expected {
		id := mustStartDialID(t, fixture.p)
		mustDialFailed(t, fixture.p, id)
	}

	connectIDs := expected[:fixture.p.cfg.MaxOut]
	for _, id := range connectIDs {
		mustStartDialID(t, fixture.p)
		mustConnectOutbound(t, fixture.p, id)
	}

	lowest := fixture.p.priority(connectIDs[0])
	for _, id := range connectIDs[1:] {
		if p := fixture.p.priority(id); p < lowest {
			lowest = p
		}
	}

	var better, worse types.NodeID
	for better == "" || worse == "" {
		id := makeNodeID(rng)
		allowed[id] = true
		priority := fixture.p.priority(id)
		fixture.register(rng, id)
		addr := fixture.addrs[id]
		fixture.p.PushPex(utils.None[types.NodeID](), []NodeAddress{addr})
		switch {
		case priority > lowest && better == "":
			better = id
		case priority < lowest && worse == "":
			worse = id
		default:
			delete(fixture.allowed, id)
		}
	}

	mustFailStartDial(t, fixture.p)
	grantUpgradePermit(fixture.p)
	id := mustStartDialID(t, fixture.p)
	require.Equal(t, better, id)
	mustDialFailed(t, fixture.p, id)
}

// Test checking that interleaving PushPex and TryStartDial works as intended:
// - pushed addresses are immediately available.
func TestPoolManagerPushPexIsImmediate(t *testing.T) {
	rng := utils.TestRng()
	allowed := map[types.NodeID]bool{}
	fixture := newPoolFixture(rng, 1, 1, allowed)
	mustFailStartDial(t, fixture.p)

	id := makeNodeID(rng)
	allowed[id] = true
	addr := fixture.register(rng, id)
	fixture.p.PushPex(utils.None[types.NodeID](), []NodeAddress{addr})

	dialed := mustStartDialID(t, fixture.p)
	require.Equal(t, id, dialed)
}

// Test checking that inbound and outbound connection for the same NodeID can coexist.
func TestPoolManagerInboundOutboundCoexist(t *testing.T) {
	rng := utils.TestRng()
	id := makeNodeID(rng)
	allowed := map[types.NodeID]bool{id: true}
	fixture := newPoolFixture(rng, 1, 1, allowed, id)

	first := mustStartDialID(t, fixture.p)
	require.Equal(t, id, first)

	mustConnectInbound(t, fixture.p, id)
	evicted, err := fixture.p.Connect(connID{NodeID: id, outbound: true})
	require.NoError(t, err)
	require.False(t, evicted.IsPresent())
}

// Test checking that InPool filter works as intended:
//   - if PushPex/FixedAddrs inserts a mix of addresses form the pool and not from the pool,
//     filtered out entries should be never dialed.
//   - inbound connections not from the pool should be rejected.
func TestPoolManagerInPoolFilter(t *testing.T) {
	rng := utils.TestRng()
	allowedID := makeNodeID(rng)
	allowed := map[types.NodeID]bool{allowedID: true}
	fixture := newPoolFixture(rng, 1, 1, allowed, allowedID)
	disallowedID := makeNodeID(rng)

	addr := makeAddrFor(rng, disallowedID)

	mixed := []NodeAddress{fixture.addrs[allowedID], addr}
	fixture.p.PushPex(utils.None[types.NodeID](), mixed)

	for range 3 {
		id := mustStartDialID(t, fixture.p)
		require.Equal(t, allowedID, id)
		mustDialFailed(t, fixture.p, id)
	}

	_, err := fixture.p.Connect(connID{NodeID: disallowedID, outbound: false})
	require.ErrorIs(t, err, errNotInPool)
}

// Test checking that InPool filter does not apply to PushPex sender.

package p2p

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestPool_AddAddr_deduplicate(t *testing.T) {
	rng := utils.TestRng()
	selfID := makeNodeID(rng)
	p := newPool[*fakeConn](poolConfig{selfID: selfID})
	require.False(t, p.AddAddr(makeAddrFor(rng,selfID)))
	require.Nil(t, p.addrs[selfID])

	peer := makeNodeID(rng)
	addr := makeAddrFor(rng, peer)
	require.True(t, p.AddAddr(addr))
	require.False(t, p.AddAddr(addr))
	require.Len(t, p.addrs[peer].addrs, 1)
}

func TestPool_AddAddr_prune_failed_addrs(t *testing.T) {
	rng := utils.TestRng()
	selfID := makeNodeID(rng)
	p := newPool[*fakeConn](poolConfig{
		selfID: selfID,
		maxAddrsPerPeer: utils.Some(1),
	})
	peer := makeNodeID(rng)
	for range 3 {
		// Insert a new address which should replace the old one.
		addr := makeAddrFor(rng, peer)
		require.True(t, p.AddAddr(addr))
		
		// Dial and fail multiple times.
		// Only the newest address is expected, since maxAddrsPerPeer == 1
		for range 5 {
			dialAddr,ok := p.TryStartDial()
			require.Equal(t, utils.Some(addr), opt(dialAddr,ok))
			p.DialFailed(dialAddr)
		}
	}
}

func TestPool_AddAddr_prune_failed_peers(t *testing.T) {
	rng := utils.TestRng()
	selfID := makeNodeID(rng)
	p := newPool[*fakeConn](poolConfig{
		selfID: selfID,
		maxAddrs: utils.Some(1),
	})
	for range 3 {
		peer := makeNodeID(rng)
		// Insert a new peer which should replace the old one.
		addr := makeAddrFor(rng, peer)
		require.True(t, p.AddAddr(addr))
		
		// Dial and fail multiple times.
		// Only the newest address is expected, since maxAddrsPerPeer == 1
		for range 5 {
			dialAddr,ok := p.TryStartDial()
			require.Equal(t, utils.Some(addr), opt(dialAddr,ok))
			p.DialFailed(dialAddr)
		}
	}
}

func TestPool_TryStartDial_RoundRobin(t *testing.T) {
	rng := utils.TestRng()
	p := newPool[*fakeConn](poolConfig{selfID: makeNodeID(rng)})

	addrs := map[NodeAddress]bool{}
	for range 10 {
		id := makeNodeID(rng)
		for range rng.Intn(5) + 1 {
			addr := makeAddrFor(rng, id)
			addrs[addr] = true
			p.AddAddr(addr)
		}
	}
	// Go through all addresses multiple times.
	for range 3 {
		// Dial until all addresses are used up.
		dialed := map[NodeAddress]bool{}
		for len(dialed) < len(addrs) {
			addr, ok := p.TryStartDial()
			require.True(t, ok)
			require.True(t, addrs[addr])
			dialed[addr] = true
			p.DialFailed(addr)
		}
	}
}

func TestPool_Connected_deduplicate(t *testing.T) {
	rng := utils.TestRng()
	p := newPool[*fakeConn](poolConfig{selfID: makeNodeID(rng)})
	peer := makeNodeID(rng)
	oldConn := makeConnFor(rng,peer,utils.GenBool(rng))
	require.NoError(t, p.Connected(oldConn))
	for range 100 {
		require.False(t, oldConn.Closed())
		newConn := makeConnFor(rng,peer,utils.GenBool(rng))
		if err := p.Connected(newConn); err==nil {
			newConn,oldConn = oldConn,newConn
		}
		require.True(t, newConn.Closed())
		p.Disconnected(newConn)
	}
}

func opt[T any](v T, ok bool) utils.Option[T] {
	if ok { return utils.Some(v) }
	return utils.None[T]()
}

func TestPool_Connected_race(t *testing.T) {
	rng := utils.TestRng()
	for range 100 {
		// There are 2 peers
		p1 := newPool[*fakeConn](poolConfig{selfID: makeNodeID(rng)})
		p2 := newPool[*fakeConn](poolConfig{selfID: makeNodeID(rng)})
		// They know each others addresses.
		p1addr := makeAddrFor(rng,p1.selfID)
		p2addr := makeAddrFor(rng,p2.selfID)
		require.True(t,p1.AddAddr(p2addr))
		require.True(t,p2.AddAddr(p1addr))
		// They dial each other.
		require.Equal(t,utils.Some(p2addr),opt(p1.TryStartDial()))
		require.Equal(t,utils.Some(p1addr),opt(p2.TryStartDial()))
		p1c1,p2c1 := makeConnFor(rng,p2.selfID,false),makeConnTo(p1addr)
		p1c2,p2c2 := makeConnTo(p2addr),makeConnFor(rng,p1.selfID,false)
		// Connections are established in random order on each side. 
		conns := utils.Slice(p1c1,p2c1,p1c2,p2c2)
		utils.Shuffle(rng,conns)
		for _,c := range conns {
			switch c.info.ID {
			case p1.selfID: _ = p2.Connected(c)
			case p2.selfID: _ = p1.Connected(c)
			}
		}
		// Connections should be closed consistently on both sides.
		require.Equal(t,p1c1.Closed(),p2c1.Closed())
		require.Equal(t,p1c2.Closed(),p2c2.Closed())
		// One connection should survice.
		require.Equal(t,p1c1.Closed(),!p1c2.Closed())
	}
}

func TestPool_Evict(t *testing.T) {
	rng := utils.TestRng()
	p := newPool[*fakeConn](poolConfig{selfID: makeNodeID(rng)})
	// Dial a peer and connect.
	peer := makeNodeID(rng)
	addr := makeAddrFor(rng, peer)
	require.True(t, p.AddAddr(addr))
	require.Equal(t,utils.Some(addr),opt(p.TryStartDial()))
	conn := makeConnTo(addr)
	require.NoError(t, p.Connected(conn))

	// Evict the peer. Connection should be closed, and the address removed.
	p.Evict(peer)
	require.True(t, conn.Closed())
	require.Equal(t, utils.None[NodeAddress](), opt(p.TryStartDial()))
}

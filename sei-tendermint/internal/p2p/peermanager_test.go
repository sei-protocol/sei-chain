package p2p

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/assert"
	dbm "github.com/tendermint/tm-db"

	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/types"
)

var selfID = makeInfo(makeKey()).NodeID

func testAddr(x string) NodeAddress {
	if len(x) != 1 {
		panic("x must be a single character")
	}
	return NodeAddress{
		NodeID:   types.NodeID(strings.Repeat(x, 40)),
		Hostname: fmt.Sprintf("%v.com", x),
		Port:     26657,
	}
}

// FIXME: We should probably have some randomized property-based tests for the
// PeerManager too, which runs a bunch of random operations with random peers
// and ensures certain invariants always hold. The logic can be complex, with
// many interactions, and it's hard to cover all scenarios with handwritten
// tests.

func TestPeerManagerOptions_Validate(t *testing.T) {
	nodeID := types.NodeID("00112233445566778899aabbccddeeff00112233")

	testcases := map[string]struct {
		options PeerManagerOptions
		ok      bool
	}{
		"zero options is valid": {PeerManagerOptions{}, true},

		// PersistentPeers
		"valid PersistentPeers NodeID": {PeerManagerOptions{
			PersistentPeers: []types.NodeID{"00112233445566778899aabbccddeeff00112233"},
		}, true},
		"invalid PersistentPeers NodeID": {PeerManagerOptions{
			PersistentPeers: []types.NodeID{"foo"},
		}, false},
		"uppercase PersistentPeers NodeID": {PeerManagerOptions{
			PersistentPeers: []types.NodeID{"00112233445566778899AABBCCDDEEFF00112233"},
		}, false},
		"PersistentPeers at MaxConnected": {PeerManagerOptions{
			PersistentPeers: []types.NodeID{nodeID, nodeID, nodeID},
			MaxConnected:    3,
		}, true},
		"PersistentPeers above MaxConnected": {PeerManagerOptions{
			PersistentPeers: []types.NodeID{nodeID, nodeID, nodeID},
			MaxConnected:    2,
		}, false},
		"PersistentPeers above MaxConnected below MaxConnectedUpgrade": {PeerManagerOptions{
			PersistentPeers:     []types.NodeID{nodeID, nodeID, nodeID},
			MaxConnected:        2,
			MaxConnectedUpgrade: 2,
		}, false},

		// MaxPeers
		"MaxPeers without MaxConnected": {PeerManagerOptions{
			MaxPeers: 3,
		}, false},
		"MaxPeers below MaxConnected+MaxConnectedUpgrade": {PeerManagerOptions{
			MaxPeers:            2,
			MaxConnected:        2,
			MaxConnectedUpgrade: 1,
		}, false},
		"MaxPeers at MaxConnected+MaxConnectedUpgrade": {PeerManagerOptions{
			MaxPeers:            3,
			MaxConnected:        2,
			MaxConnectedUpgrade: 1,
		}, true},

		// MaxRetryTime
		"MaxRetryTime below MinRetryTime": {PeerManagerOptions{
			MinRetryTime: 7 * time.Second,
			MaxRetryTime: 5 * time.Second,
		}, false},
		"MaxRetryTime at MinRetryTime": {PeerManagerOptions{
			MinRetryTime: 5 * time.Second,
			MaxRetryTime: 5 * time.Second,
		}, true},
		"MaxRetryTime without MinRetryTime": {PeerManagerOptions{
			MaxRetryTime: 5 * time.Second,
		}, false},

		// MaxRetryTimePersistent
		"MaxRetryTimePersistent below MinRetryTime": {PeerManagerOptions{
			MinRetryTime:           7 * time.Second,
			MaxRetryTimePersistent: 5 * time.Second,
		}, false},
		"MaxRetryTimePersistent at MinRetryTime": {PeerManagerOptions{
			MinRetryTime:           5 * time.Second,
			MaxRetryTimePersistent: 5 * time.Second,
		}, true},
		"MaxRetryTimePersistent without MinRetryTime": {PeerManagerOptions{
			MaxRetryTimePersistent: 5 * time.Second,
		}, false},
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

func TestNewPeerManager(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")

	// Zero options should be valid.
	_, err := NewPeerManager(logger, selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
	require.NoError(t, err)

	// Invalid options should error.
	_, err = NewPeerManager(logger, selfID, dbm.NewMemDB(), PeerManagerOptions{
		PersistentPeers: []types.NodeID{"foo"},
	}, NopMetrics())
	require.Error(t, err)
}

func TestNewPeerManager_Persistence(t *testing.T) {
	aID := types.NodeID(strings.Repeat("a", 40))
	aAddresses := []NodeAddress{
		{NodeID: aID, Hostname: "127.0.0.1", Port: 26657},
	}

	bID := types.NodeID(strings.Repeat("b", 40))
	bAddresses := []NodeAddress{
		{NodeID: bID, Hostname: "b10c::1", Port: 26657},
	}

	cID := types.NodeID(strings.Repeat("c", 40))
	cAddresses := []NodeAddress{
		{NodeID: cID, Hostname: "host.domain", Port: 80},
	}

	// Create an initial peer manager and add the peers.
	db := dbm.NewMemDB()
	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, db, PeerManagerOptions{
		PersistentPeers: []types.NodeID{aID},
		PeerScores:      map[types.NodeID]PeerScore{bID: 1},
	}, NopMetrics())
	require.NoError(t, err)

	for _, addr := range append(append(aAddresses, bAddresses...), cAddresses...) {
		added, err := peerManager.Add(addr)
		require.NoError(t, err)
		require.True(t, added)
	}

	require.ElementsMatch(t, aAddresses, peerManager.Addresses(aID))
	require.ElementsMatch(t, bAddresses, peerManager.Addresses(bID))
	require.ElementsMatch(t, cAddresses, peerManager.Addresses(cID))

	require.Equal(t, map[types.NodeID]PeerScore{
		aID: PeerScorePersistent,
		bID: 1,
		cID: DefaultMutableScore,
	}, peerManager.Scores())

	// Creating a new peer manager with the same database should retain the
	// peers, but they should have updated scores from the new PersistentPeers
	// configuration.
	peerManager, err = NewPeerManager(log.NewNopLogger(), selfID, db, PeerManagerOptions{
		PersistentPeers: []types.NodeID{bID},
		PeerScores:      map[types.NodeID]PeerScore{cID: 1},
	}, NopMetrics())
	require.NoError(t, err)

	require.ElementsMatch(t, aAddresses, peerManager.Addresses(aID))
	require.ElementsMatch(t, bAddresses, peerManager.Addresses(bID))
	require.ElementsMatch(t, cAddresses, peerManager.Addresses(cID))
	require.Equal(t, map[types.NodeID]PeerScore{
		aID: 0,
		bID: PeerScorePersistent,
		cID: 1,
	}, peerManager.Scores())

	// Introduce a dial failure and persistent peer score should be reduced by one
	ctx := t.Context()
	peerManager.DialFailed(ctx, bAddresses[0])
	require.Equal(t, map[types.NodeID]PeerScore{
		aID: 0,
		bID: PeerScorePersistent,
		cID: 1,
	}, peerManager.Scores())
}

func TestNewPeerManager_Unconditional(t *testing.T) {
	aID := types.NodeID(strings.Repeat("a", 40))
	aAddresses := []NodeAddress{
		{NodeID: aID, Hostname: "127.0.0.1", Port: 26657},
	}

	bID := types.NodeID(strings.Repeat("b", 40))
	bAddresses := []NodeAddress{
		{NodeID: bID, Hostname: "b10c::1", Port: 26657},
	}

	cID := types.NodeID(strings.Repeat("c", 40))
	cAddresses := []NodeAddress{
		{NodeID: cID, Hostname: "host.domain", Port: 80},
	}

	// Create an initial peer manager and add the peers.
	db := dbm.NewMemDB()
	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, db, PeerManagerOptions{
		UnconditionalPeers: []types.NodeID{aID},
		PeerScores:         map[types.NodeID]PeerScore{bID: 1},
	}, NopMetrics())
	require.NoError(t, err)

	for _, addr := range append(append(aAddresses, bAddresses...), cAddresses...) {
		added, err := peerManager.Add(addr)
		require.NoError(t, err)
		require.True(t, added)
	}

	require.ElementsMatch(t, aAddresses, peerManager.Addresses(aID))
	require.ElementsMatch(t, bAddresses, peerManager.Addresses(bID))
	require.ElementsMatch(t, cAddresses, peerManager.Addresses(cID))
	require.Equal(t, map[types.NodeID]PeerScore{
		aID: PeerScoreUnconditional,
		bID: 1,
		cID: DefaultMutableScore,
	}, peerManager.Scores())

	// Creating a new peer manager with the same database should retain the
	// peers, but they should have updated scores from the new PersistentPeers
	// configuration.
	peerManager, err = NewPeerManager(log.NewNopLogger(), selfID, db, PeerManagerOptions{
		UnconditionalPeers: []types.NodeID{bID},
		PeerScores:         map[types.NodeID]PeerScore{cID: 1},
	}, NopMetrics())
	require.NoError(t, err)

	require.ElementsMatch(t, aAddresses, peerManager.Addresses(aID))
	require.ElementsMatch(t, bAddresses, peerManager.Addresses(bID))
	require.ElementsMatch(t, cAddresses, peerManager.Addresses(cID))
	require.Equal(t, map[types.NodeID]PeerScore{
		aID: 0,
		bID: PeerScoreUnconditional,
		cID: 1,
	}, peerManager.Scores())
}

func TestPeerManager_Add(t *testing.T) {
	aID := types.NodeID(strings.Repeat("a", 40))
	bID := types.NodeID(strings.Repeat("b", 40))
	cID := types.NodeID(strings.Repeat("c", 40))

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
		PersistentPeers: []types.NodeID{aID, cID},
		MaxPeers:        2,
		MaxConnected:    2,
	}, NopMetrics())
	require.NoError(t, err)

	// Adding a couple of addresses should work.
	aAddresses := []NodeAddress{
		{NodeID: aID, Hostname: "localhost"},
	}
	for _, addr := range aAddresses {
		added, err := peerManager.Add(addr)
		require.NoError(t, err)
		require.True(t, added)
	}
	require.ElementsMatch(t, aAddresses, peerManager.Addresses(aID))

	// Adding a different peer should be fine.
	bAddress := NodeAddress{NodeID: bID, Hostname: "localhost"}
	added, err := peerManager.Add(bAddress)
	require.NoError(t, err)
	require.True(t, added)
	require.Equal(t, []NodeAddress{bAddress}, peerManager.Addresses(bID))
	require.ElementsMatch(t, aAddresses, peerManager.Addresses(aID))

	// Adding an existing address again should be a noop.
	added, err = peerManager.Add(aAddresses[0])
	require.NoError(t, err)
	require.False(t, added)
	require.ElementsMatch(t, aAddresses, peerManager.Addresses(aID))

	// Adding a third peer with MaxPeers=2 should cause bID, which is
	// the lowest-scored peer (not in PersistentPeers), to be removed.
	added, err = peerManager.Add(NodeAddress{NodeID: cID, Hostname: "localhost"})
	require.NoError(t, err)
	require.True(t, added)
	require.ElementsMatch(t, []types.NodeID{aID, cID}, peerManager.Peers())

	// Adding an invalid address should error.
	_, err = peerManager.Add(NodeAddress{})
	require.Error(t, err)
}

func TestPeerManager_DialNext(t *testing.T) {
	ctx := t.Context()

	a := testAddr("a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
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

	a := testAddr("a")

	options := PeerManagerOptions{
		MinRetryTime: 100 * time.Millisecond,
		MaxRetryTime: 1000 * time.Millisecond,
	}
	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), options, NopMetrics())
	require.NoError(t, err)

	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)

	// Do five dial retries (six dials total). The retry time should double for
	// each failure. At the forth retry, MaxRetryTime should kick in.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

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

func TestPeerManagerDeleteOnMaxRetries(t *testing.T) {
	ctx := t.Context()

	a := testAddr("a")

	options := PeerManagerOptions{
		MinRetryTime: 100 * time.Millisecond,
		MaxRetryTime: 1000 * time.Millisecond,
	}
	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), options, NopMetrics())
	require.NoError(t, err)

	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)

	// Do five dial retries (six dials total). The retry time should double for
	// each failure. At the forth retry, MaxRetryTime should kick in.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	for i := 0; i < 4; i++ {
		start := time.Now()
		dial, err := peerManager.DialNext(ctx)
		require.NoError(t, err)
		require.Equal(t, a, dial)
		elapsed := time.Since(start).Round(time.Millisecond)
		if i > 0 {
			require.GreaterOrEqual(t, elapsed, time.Duration(math.Pow(2, float64(i)))*options.MinRetryTime)
		}
		if i == 3 {
			if got, err := (DialFailuresError{}), peerManager.DialFailed(ctx, a); !errors.As(err, &got) || got.Failures != 4 {
				t.Errorf("expected 4 failures, got error %v", err)
			}

			continue
		}
		require.NoError(t, peerManager.DialFailed(ctx, a))
	}
}

func TestPeerManager_DialNext_WakeOnDialFailed(t *testing.T) {
	ctx := t.Context()

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
		MaxConnected: 1,
	}, NopMetrics())
	require.NoError(t, err)

	a := testAddr("a")
	b := testAddr("b")

	// Add and dial a.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, a, dial)

	// Add b. We shouldn't be able to dial it, due to MaxConnected.
	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Zero(t, dial)

	// Spawn a goroutine to fail a's dial attempt.
	sig := make(chan struct{})
	go func() {
		defer close(sig)
		time.Sleep(200 * time.Millisecond)
		require.NoError(t, peerManager.DialFailed(ctx, a))
	}()

	// This should make b available for dialing (not a, retries are disabled).
	opctx, opcancel := context.WithTimeout(ctx, 3*time.Second)
	defer opcancel()
	dial, err = peerManager.DialNext(opctx)
	require.NoError(t, err)
	require.Equal(t, b, dial)
	<-sig
}

func TestPeerManager_DialNext_WakeOnDialFailedRetry(t *testing.T) {
	ctx := t.Context()

	options := PeerManagerOptions{MinRetryTime: 200 * time.Millisecond}
	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), options, NopMetrics())
	require.NoError(t, err)

	a := testAddr("a")

	// Add a, dial it, and mark it a failure. This will start a retry timer.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, a, dial)
	require.NoError(t, peerManager.DialFailed(ctx, dial))
	failed := time.Now()

	// The retry timer should unblock DialNext and make a available again after
	// the retry time passes.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	dial, err = peerManager.DialNext(ctx)
	require.NoError(t, err)
	require.Equal(t, a, dial)
	require.GreaterOrEqual(t, time.Since(failed), options.MinRetryTime)
}

func TestPeerManager_DialNext_WakeOnDisconnected(t *testing.T) {
	ctx := t.Context()

	a := testAddr("a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
	require.NoError(t, err)

	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	err = peerManager.Accepted(a.NodeID)
	require.NoError(t, err)

	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Zero(t, dial)

	dctx, dcancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer dcancel()
	go func() {
		time.Sleep(200 * time.Millisecond)
		peerManager.Disconnected(dctx, a.NodeID)
	}()

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	dial, err = peerManager.DialNext(ctx)
	require.NoError(t, err)
	require.Equal(t, a, dial)
}

func TestPeerManager_TryDialNext_MaxConnected(t *testing.T) {
	a := testAddr("a")
	b := testAddr("b")
	c := testAddr("c")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
		MaxConnected: 2,
	}, NopMetrics())
	require.NoError(t, err)

	// Add a and connect to it.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, a, dial)
	require.NoError(t, peerManager.Dialed(a))

	// Add b and start dialing it.
	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, b, dial)

	// At this point, adding c will not allow dialing it.
	added, err = peerManager.Add(c)
	require.NoError(t, err)
	require.True(t, added)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Zero(t, dial)
}

func TestPeerManager_TryDialNext_MaxConnectedUpgrade(t *testing.T) {
	ctx := t.Context()

	a := testAddr("a")
	b := testAddr("b")
	c := testAddr("c")
	d := testAddr("d")
	e := testAddr("e")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
		PeerScores: map[types.NodeID]PeerScore{
			a.NodeID: 10,
			b.NodeID: 11,
			c.NodeID: 12,
			d.NodeID: 13,
			e.NodeID: 10,
		},
		PersistentPeers:     []types.NodeID{c.NodeID, d.NodeID},
		MaxConnected:        2,
		MaxConnectedUpgrade: 1,
	}, NopMetrics())
	require.NoError(t, err)

	// Add a and connect to it.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, a, dial)
	require.NoError(t, peerManager.Dialed(a))

	// Add b and start dialing it.
	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, b, dial)

	// Even though we are at capacity, we should be allowed to dial c for an
	// upgrade of a, since it's higher-scored.
	added, err = peerManager.Add(c)
	require.NoError(t, err)
	require.True(t, added)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, c, dial)

	// However, since we're using all upgrade slots now, we can't add and dial
	// d, even though it's also higher-scored.
	added, err = peerManager.Add(d)
	require.NoError(t, err)
	require.True(t, added)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Zero(t, dial)

	// We go through with c's upgrade.
	require.NoError(t, peerManager.Dialed(c))

	// Still can't dial d.
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Zero(t, dial)

	// Now, if we disconnect a, we should be allowed to dial d because we have a
	// free upgrade slot.
	peerManager.Disconnected(ctx, a.NodeID)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, d, dial)
	require.NoError(t, peerManager.Dialed(d))

	// However, if we disconnect b (such that only c and d are connected), we
	// should not be allowed to dial e even though there are upgrade slots,
	// because there are no lower-scored nodes that can be upgraded.
	peerManager.Disconnected(ctx, b.NodeID)
	added, err = peerManager.Add(e)
	require.NoError(t, err)
	require.True(t, added)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Zero(t, dial)
}

func TestPeerManager_TryDialNext_UpgradeReservesPeer(t *testing.T) {
	a := testAddr("a")
	b := testAddr("b")
	c := testAddr("c")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
		PeerScores:          map[types.NodeID]PeerScore{b.NodeID: DefaultMutableScore + 1, c.NodeID: DefaultMutableScore + 1},
		MaxConnected:        1,
		MaxConnectedUpgrade: 2,
	}, NopMetrics())
	require.NoError(t, err)

	// Add a and connect to it.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, a, dial)
	require.NoError(t, peerManager.Dialed(a))

	// Add b and start dialing it. This will claim a for upgrading.
	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, b, dial)

	// Adding c and dialing it will fail, because a is the only connected
	// peer that can be upgraded, and b is already trying to upgrade it.
	added, err = peerManager.Add(c)
	require.NoError(t, err)
	require.True(t, added)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Empty(t, dial)
}

func TestPeerManager_TryDialNext_DialingConnected(t *testing.T) {
	a := testAddr("a")
	a2 := a
	a2.Hostname = "else.com"
	b := testAddr("b")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
		MaxConnected: 2,
	}, NopMetrics())
	require.NoError(t, err)

	// Add a and dial it.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, a, dial)

	// Adding a's TCP address will not dispense a, since it's already dialing.
	added, err = peerManager.Add(a2)
	require.NoError(t, err)
	require.True(t, added)
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Zero(t, dial)

	// Marking a as dialed will still not dispense it.
	require.NoError(t, peerManager.Dialed(a))
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Zero(t, dial)

	// Adding b and accepting a connection from it will not dispense it either.
	added, err = peerManager.Add(b)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(b.NodeID))
	dial, err = peerManager.TryDialNext()
	require.NoError(t, err)
	require.Zero(t, dial)
}

func TestPeerManager_TryDialNext_Multiple(t *testing.T) {
	ctx := t.Context()

	aID := types.NodeID(strings.Repeat("a", 40))
	bID := types.NodeID(strings.Repeat("b", 40))
	addresses := []NodeAddress{
		{NodeID: aID, Hostname: "127.0.0.1"},
		{NodeID: bID, Hostname: "::1"},
	}

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
	require.NoError(t, err)

	for _, address := range addresses {
		added, err := peerManager.Add(address)
		require.NoError(t, err)
		require.True(t, added)
	}

	// All addresses should be dispensed as long as dialing them has failed.
	dial := []NodeAddress{}
	for range addresses {
		address, err := peerManager.TryDialNext()
		require.NoError(t, err)
		require.NotZero(t, address)
		require.NoError(t, peerManager.DialFailed(ctx, address))
		dial = append(dial, address)
	}
	require.ElementsMatch(t, dial, addresses)

	address, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Zero(t, address)
}

func TestPeerManager_DialFailed(t *testing.T) {
	// DialFailed is tested through other tests, we'll just check a few basic
	// things here, e.g. reporting unknown addresses.
	a := testAddr("a")
	b := testAddr("b")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
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

	a := testAddr("a")
	b := testAddr("b")
	c := testAddr("c")

	logger, _ := log.NewDefaultLogger("plain", "debug")
	peerManager, err := NewPeerManager(logger, selfID, dbm.NewMemDB(), PeerManagerOptions{
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
	a := testAddr("a")
	b := testAddr("b")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
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
	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
	require.NoError(t, err)

	// Dialing self should error.
	added, err := peerManager.Add(NodeAddress{NodeID: selfID, Hostname: "a.com", Port: 1234})
	require.Nil(t, err)
	require.False(t, added)
}

func TestPeerManager_Dialed_MaxConnected(t *testing.T) {
	a := testAddr("a")
	b := testAddr("b")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
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
	a := testAddr("a")
	b := testAddr("b")
	c := testAddr("c")
	d := testAddr("d")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
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
	a := testAddr("a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
	require.NoError(t, err)

	// Marking an unknown node as dialed should error.
	require.Error(t, peerManager.Dialed(a))
}

func TestPeerManager_Dialed_Upgrade(t *testing.T) {
	a := testAddr("a")
	b := testAddr("b")
	c := testAddr("c")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
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

	a := testAddr("a")
	b := testAddr("b")
	c := testAddr("c")
	d := testAddr("d")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
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

	a := testAddr("a")
	b := testAddr("b")
	c := testAddr("c")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
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
	a := testAddr("a")
	b := testAddr("b")
	c := testAddr("c")
	d := testAddr("d")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
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
	a := testAddr("a")
	b := testAddr("b")
	c := testAddr("c")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
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
	a := testAddr("a")
	b := testAddr("b")
	c := testAddr("c")
	d := testAddr("d")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
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

	a := testAddr("a")
	b := testAddr("b")
	c := testAddr("c")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
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
	a := testAddr("a")
	b := testAddr("b")
	c := testAddr("c")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
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
	a := testAddr("a")
	b := testAddr("b")

	ctx := t.Context()

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
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

	pm, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
	require.NoError(t, err)

	sub := pm.Subscribe(ctx)

	a := testAddr("a")
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

	a := testAddr("a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
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

	a := testAddr("a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
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

	a := testAddr("a")
	b := testAddr("b")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
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

	a := testAddr("a")
	b := testAddr("b")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
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

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
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
	a := testAddr("a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
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

func TestPeerManager_Errored(t *testing.T) {
	ctx := t.Context()

	a := testAddr("a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
	require.NoError(t, err)

	// Erroring an unknown peer does nothing.
	peerManager.Errored(a.NodeID, errors.New("foo"))
	require.Empty(t, peerManager.Peers())
	evict, err := peerManager.TryEvictNext()
	require.NoError(t, err)
	require.Zero(t, evict)

	// Erroring a known peer does nothing, and won't evict it later,
	// even when it connects.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	peerManager.Errored(a.NodeID, errors.New("foo"))
	evict, err = peerManager.TryEvictNext()
	require.NoError(t, err)
	require.Zero(t, evict)

	require.NoError(t, peerManager.Accepted(a.NodeID))
	peerManager.Ready(ctx, a.NodeID, nil)
	evict, err = peerManager.TryEvictNext()
	require.NoError(t, err)
	require.Zero(t, evict)

	// However, erroring once connected will evict it.
	peerManager.Errored(a.NodeID, errors.New("foo"))
	evict, err = peerManager.TryEvictNext()
	require.NoError(t, err)
	if ev, ok := evict.Get(); !ok || ev.ID != a.NodeID {
		t.Fatalf("evict = %v, expected %s", evict, a.NodeID)
	}
}

func TestPeerManager_Subscribe(t *testing.T) {
	ctx := t.Context()

	a := testAddr("a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
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
}

func TestPeerManager_Subscribe_Close(t *testing.T) {
	ctx := t.Context()

	a := testAddr("a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
	require.NoError(t, err)

	sub := peerManager.Subscribe(ctx)

	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(a.NodeID))
	require.Empty(t, sub.Updates())

	peerManager.Ready(ctx, a.NodeID, nil)
	require.NotEmpty(t, sub.Updates())
	require.Equal(t, PeerUpdate{NodeID: a.NodeID, Status: PeerStatusUp}, <-sub.Updates())

	// Closing the subscription should not send us the disconnected update.
	t.Cleanup(func() {
		peerManager.Disconnected(ctx, a.NodeID)
		require.Empty(t, sub.Updates())
	})
}

func TestPeerManager_Subscribe_Broadcast(t *testing.T) {
	ctx := t.Context()

	t.Cleanup(leaktest.Check(t))

	a := testAddr("a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{}, NopMetrics())
	require.NoError(t, err)

	s2ctx, s2cancel := context.WithCancel(ctx)
	defer s2cancel()

	s1 := peerManager.Subscribe(ctx)
	s2 := peerManager.Subscribe(s2ctx)
	s3 := peerManager.Subscribe(ctx)

	// Connecting to a peer should send updates on all subscriptions.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, peerManager.Accepted(a.NodeID))
	peerManager.Ready(ctx, a.NodeID, nil)

	expectUp := PeerUpdate{NodeID: a.NodeID, Status: PeerStatusUp}
	require.NotEmpty(t, s1)
	require.Equal(t, expectUp, <-s1.Updates())
	require.NotEmpty(t, s2)
	require.Equal(t, expectUp, <-s2.Updates())
	require.NotEmpty(t, s3)
	require.Equal(t, expectUp, <-s3.Updates())

	// We now close s2. Disconnecting the peer should only send updates
	// on s1 and s3.
	s2cancel()
	time.Sleep(250 * time.Millisecond) // give the thread a chance to exit
	peerManager.Disconnected(ctx, a.NodeID)

	expectDown := PeerUpdate{NodeID: a.NodeID, Status: PeerStatusDown}
	require.NotEmpty(t, s1)
	require.Equal(t, expectDown, <-s1.Updates())
	require.Empty(t, s2.Updates())
	require.NotEmpty(t, s3)
	require.Equal(t, expectDown, <-s3.Updates())
}

func TestPeerManager_Close(t *testing.T) {
	// leaktest will check that spawned goroutines are closed.
	t.Cleanup(leaktest.CheckTimeout(t, 1*time.Second))

	ctx := t.Context()

	a := testAddr("a")

	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
		MinRetryTime: 10 * time.Second,
	}, NopMetrics())
	require.NoError(t, err)

	// This subscription isn't closed, but PeerManager.Close()
	// should reap the spawned goroutine.
	_ = peerManager.Subscribe(ctx)

	// This dial failure will start a retry timer for 10 seconds, which
	// should be reaped.
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)
	dial, err := peerManager.TryDialNext()
	require.NoError(t, err)
	require.Equal(t, a, dial)
	require.NoError(t, peerManager.DialFailed(ctx, a))
}

func TestPeerManager_Advertise(t *testing.T) {
	aID := types.NodeID(strings.Repeat("a", 40))
	aTCP := NodeAddress{NodeID: aID, Hostname: "127.0.0.1", Port: 26657}

	bID := types.NodeID(strings.Repeat("b", 40))
	bTCP := NodeAddress{NodeID: bID, Hostname: "b10c::1", Port: 26657}

	cID := types.NodeID(strings.Repeat("c", 40))
	cTCP := NodeAddress{NodeID: cID, Hostname: "host.domain", Port: 80}

	dID := types.NodeID(strings.Repeat("d", 40))

	// Create an initial peer manager and add the peers.
	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
		PeerScores: map[types.NodeID]PeerScore{aID: 3, bID: 2, cID: 1},
	}, NopMetrics())
	require.NoError(t, err)

	added, err := peerManager.Add(aTCP)
	require.NoError(t, err)
	require.True(t, added)
	added, err = peerManager.Add(bTCP)
	require.NoError(t, err)
	require.True(t, added)
	added, err = peerManager.Add(cTCP)
	require.NoError(t, err)
	require.True(t, added)

	// d should get all addresses.
	require.ElementsMatch(t, []NodeAddress{
		aTCP, bTCP, cTCP,
	}, peerManager.Advertise(dID, 100))

	// a should not get its own addresses.
	require.ElementsMatch(t, []NodeAddress{
		bTCP, cTCP,
	}, peerManager.Advertise(aID, 100))

	// Asking for 0 addresses should return, well, 0.
	require.Empty(t, peerManager.Advertise(aID, 0))

	// Asking for 2 addresses should get the highest-rated ones, i.e. a.
	require.ElementsMatch(t, []NodeAddress{
		aTCP,
	}, peerManager.Advertise(dID, 1))
}

func TestPeerManager_Advertise_Self(t *testing.T) {
	dID := types.NodeID(strings.Repeat("d", 40))

	self := NodeAddress{NodeID: selfID, Hostname: "2001:db8::1", Port: 26657}

	// Create a peer manager with SelfAddress defined.
	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), PeerManagerOptions{
		SelfAddress: self,
	}, NopMetrics())
	require.NoError(t, err)

	// peer manager should always advertise its SelfAddress.
	require.ElementsMatch(t, []NodeAddress{
		self,
	}, peerManager.Advertise(dID, 100))
}

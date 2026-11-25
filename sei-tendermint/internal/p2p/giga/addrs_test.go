package giga

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// acceptAll is a verify function that accepts every announcement.
func acceptAll(*types.Signed[types.ValidatorAddrAnnouncement]) error { return nil }

// testAddrs is a ValidatorAddrs whose entries carry no address of their own.
type testAddrs = ValidatorAddrs[struct{}]

// testSnapshot is a snapshot of a testAddrs.
type testSnapshot = ValidatorAddrSnapshot[struct{}]

// newVerified returns a testAddrs that keeps the announcements verify accepts.
func newVerified(verify func(*types.Signed[types.ValidatorAddrAnnouncement]) error) *testAddrs {
	return NewValidatorAddrs(func(ann *types.Signed[types.ValidatorAddrAnnouncement]) (struct{}, error) {
		return struct{}{}, verify(ann)
	})
}

// newAddrs returns a ValidatorAddrs that accepts every announcement, already holding anns.
func newAddrs(anns ...*types.Signed[types.ValidatorAddrAnnouncement]) *testAddrs {
	a := newVerified(acceptAll)
	for _, ann := range anns {
		_ = a.Receive(ann)
	}
	return a
}

// hasAddr reports whether a holds an announcement of validator.
func hasAddr(a *testAddrs, validator types.PublicKey) bool {
	_, ok := a.Get(validator)
	return ok
}

// waitAddrs waits until a holds at least n announcements.
func waitAddrs(ctx context.Context, a *testAddrs, n int) error {
	_, err := a.Watch().Wait(ctx, func(s testSnapshot) bool { return len(s) >= n })
	return err
}

func signedAddr(key types.SecretKey, version uint64) *types.Signed[types.ValidatorAddrAnnouncement] {
	return key.Sign(types.ValidatorAddrAnnouncement{
		Version:  version,
		NodeKey:  "node:ed25519:public:unused",
		HostPort: "validator.example:26656",
	})
}

// pipe returns the server and client ends of an in-memory connection, both already running.
func pipe(ctx context.Context, s scope.Scope) (rpc.Server[API], rpc.Client[API]) {
	serverConn, clientConn := conn.NewTestConn()
	server := rpc.NewServer[API]()
	client := rpc.NewClient[API]()
	s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, server.Run(ctx, serverConn)) })
	s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, client.Run(ctx, clientConn)) })
	return server, client
}

// startStream connects a listener holding listener to a dialer holding dialer.
func startStream(ctx context.Context, s scope.Scope, listener, dialer *testAddrs) {
	server, client := pipe(ctx, s)
	s.SpawnBg(func() error {
		return utils.IgnoreAfterCancel(ctx, (&Service{validatorAddrs: listener}).serverStreamValidatorAddrs(ctx, server))
	})
	s.SpawnBg(func() error {
		return utils.IgnoreAfterCancel(ctx, (&Service{validatorAddrs: dialer}).clientStreamValidatorAddrs(ctx, client))
	})
}

// An announcement is not sent back to the peer it came from.
func TestValidatorAddrIsNotEchoedToItsSender(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	owner, marker := types.GenSecretKey(rng), types.GenSecretKey(rng)
	echoed := utils.NewAtomicSend(false)
	dialer := newVerified(func(ann *types.Signed[types.ValidatorAddrAnnouncement]) error {
		if ann.Key() == owner.Public() {
			echoed.Store(true)
		}
		return nil
	})
	require.NoError(t, dialer.Receive(signedAddr(owner, 1)))
	echoed.Store(false) // only receptions over the stream count as an echo
	listener := newAddrs()
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		startStream(ctx, s, listener, dialer)
		if _, err := listener.Watch().Wait(ctx, func(s testSnapshot) bool { return len(s) > 0 }); err != nil {
			return err
		}
		if err := listener.Receive(signedAddr(marker, 1)); err != nil {
			return err
		}
		// Messages on a stream arrive in order, so once the marker is here any echo of owner is too.
		_, err := dialer.Watch().Wait(ctx, func(s testSnapshot) bool { return len(s) > 1 })
		return err
	})
	require.NoError(t, err)
	require.False(t, echoed.Load())
}

// A peer holding a table for every possible member of every Anchor epoch is relayed in full.
func TestValidatorAddrStreamRelaysFullTable(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	const maxTable = len(data.Anchor{}.Epochs) * types.MaxValidators
	var anns []*types.Signed[types.ValidatorAddrAnnouncement]
	for i := range maxTable {
		anns = append(anns, signedAddr(types.GenSecretKey(rng), uint64(i+1)))
	}
	source := newAddrs(anns...)
	sink := newAddrs()
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		startStream(ctx, s, source, sink)
		_, err := sink.Watch().Wait(ctx, func(s testSnapshot) bool { return len(s) >= len(anns) })
		return err
	})
	require.NoError(t, err)
}

// Each announcement crosses a stream once, and one that joins a settled table still arrives.
func TestValidatorAddrStreamSendsEachAnnouncementOnce(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	received := utils.NewMutex(new(int))
	sink := newVerified(func(*types.Signed[types.ValidatorAddrAnnouncement]) error {
		for n := range received.Lock() {
			*n++
		}
		return nil
	})
	source := newAddrs(signedAddr(types.GenSecretKey(rng), 1))
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		startStream(ctx, s, source, sink)
		if err := waitAddrs(ctx, sink, 1); err != nil {
			return err
		}
		if err := source.Receive(signedAddr(types.GenSecretKey(rng), 1)); err != nil {
			return err
		}
		return waitAddrs(ctx, sink, 2)
	})
	require.NoError(t, err)
	for n := range received.Lock() {
		require.Equal(t, 2, *n)
	}
}

// A rejected announcement is dropped without ending the stream: a valid one sent after it still arrives.
func TestValidatorAddrStreamSurvivesRejectedAnnouncement(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	first, bad, last := types.GenSecretKey(rng), types.GenSecretKey(rng), types.GenSecretKey(rng)
	source := newAddrs(signedAddr(first, 1))
	rejections := utils.NewAtomicSend(0)
	sink := newVerified(func(ann *types.Signed[types.ValidatorAddrAnnouncement]) error {
		if ann.Key() != bad.Public() {
			return nil
		}
		rejections.Store(rejections.Load() + 1)
		return errors.New("rejected")
	})
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		startStream(ctx, s, source, sink)
		if _, err := sink.Watch().Wait(ctx, func(s testSnapshot) bool { return len(s) > 0 }); err != nil {
			return err
		}
		if err := source.Receive(signedAddr(bad, 1)); err != nil {
			return err
		}
		if _, err := rejections.Subscribe().Wait(ctx, func(n int) bool { return n > 0 }); err != nil {
			return err
		}
		if err := source.Receive(signedAddr(last, 1)); err != nil {
			return err
		}
		_, err := sink.Watch().Wait(ctx, func(s testSnapshot) bool { return len(s) > 1 })
		return err
	})
	require.NoError(t, err)
	require.True(t, hasAddr(sink, last.Public()))
	require.False(t, hasAddr(sink, bad.Public()))
}

// Every connection starts with the full table, so a peer that comes back after a network event
// is brought up to date.
func TestValidatorAddrStreamSendsFullTableOnEveryConnection(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	var anns []*types.Signed[types.ValidatorAddrAnnouncement]
	for i := range 3 {
		anns = append(anns, signedAddr(types.GenSecretKey(rng), uint64(i+1)))
	}
	source := newAddrs(anns...)
	for range 2 {
		sink := newAddrs()
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			startStream(ctx, s, source, sink)
			_, err := sink.Watch().Wait(ctx, func(s testSnapshot) bool { return len(s) >= len(anns) })
			return err
		})
		require.NoError(t, err)
		for _, ann := range anns {
			require.True(t, hasAddr(sink, ann.Key()))
		}
	}
}

// A peer that was offline during an update receives it on reconnect, including a validator it has
// never seen and a newer version of one it already held.
func TestValidatorAddrOfflinePeerReceivesMissedUpdatesOnReconnect(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	known, joiner := types.GenSecretKey(rng), types.GenSecretKey(rng)
	source := newAddrs(signedAddr(known, 1))
	sink := newAddrs()
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		startStream(ctx, s, source, sink)
		_, err := sink.Watch().Wait(ctx, func(s testSnapshot) bool { return len(s) > 0 })
		return err
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), versionOf(sink, known.Public()))

	// The sink is offline here: no stream connects it to the source.
	require.NoError(t, source.Receive(signedAddr(known, 2)))
	require.NoError(t, source.Receive(signedAddr(joiner, 1)))

	err = scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		startStream(ctx, s, source, sink)
		_, err := sink.Watch().Wait(ctx, func(testSnapshot) bool {
			return versionOf(sink, known.Public()) == 2 && hasAddr(sink, joiner.Public())
		})
		return err
	})
	require.NoError(t, err)
	require.Equal(t, uint64(2), versionOf(sink, known.Public()))
}

// versionOf returns the held announcement version of validator in a, or 0 if a holds none.
func versionOf(a *testAddrs, validator types.PublicKey) uint64 {
	if entry, ok := a.Watch().Load()[validator]; ok {
		return entry.Ann.Msg().Version
	}
	return 0
}

// A stale announcement leaves the held snapshot unchanged.
func TestValidatorAddrsKeepNewestVersion(t *testing.T) {
	rng := utils.TestRng()
	key := types.GenSecretKey(rng)
	addrs := newAddrs()
	require.NoError(t, addrs.Receive(signedAddr(key, 2)))
	before := addrs.Watch().Load()
	require.NoError(t, addrs.Receive(signedAddr(key, 1)))
	require.True(t, before.Same(addrs.Watch().Load()))
	require.Equal(t, uint64(2), versionOf(addrs, key.Public()))
}

// Retain drops the announcements of the validators it rejects, and publishes a new snapshot only then.
func TestValidatorAddrsRetain(t *testing.T) {
	rng := utils.TestRng()
	stay, leave := types.GenSecretKey(rng), types.GenSecretKey(rng)
	addrs := newAddrs(signedAddr(stay, 1), signedAddr(leave, 1))
	before := addrs.Watch().Load()
	addrs.Retain(func(types.PublicKey) bool { return true })
	require.True(t, before.Same(addrs.Watch().Load()))
	addrs.Retain(func(validator types.PublicKey) bool { return validator == stay.Public() })
	require.False(t, before.Same(addrs.Watch().Load()))
	require.True(t, hasAddr(addrs, stay.Public()))
	require.False(t, hasAddr(addrs, leave.Public()))
}

// A peer that exceeds its rate limit ends the gossip exchange, but the scope that runs the exchange
// does not fail: consensus streams on the same connection keep running.
func TestValidatorAddrExchangeEndsWithoutFailingWhenRateExceeded(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	var anns []*types.Signed[types.ValidatorAddrAnnouncement]
	for i := range 2 * validatorAddrBurst {
		anns = append(anns, signedAddr(types.GenSecretKey(rng), uint64(i+1)))
	}
	sink := newAddrs()
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		server, client := pipe(ctx, s)
		// The peer sends without pacing, which an honest sender never does.
		s.SpawnBg(func() error {
			return utils.IgnoreAfterCancel(ctx, StreamValidatorAddrs.Serve(ctx, server, func(ctx context.Context, stream validatorAddrStream) error {
				for _, ann := range anns {
					if err := stream.Send(ctx, types.SignedValidatorAddrAnnouncementConv.Encode(ann)); err != nil {
						return nil
					}
				}
				<-ctx.Done()
				return nil
			}))
		})
		return (&Service{validatorAddrs: sink}).clientStreamValidatorAddrs(ctx, client)
	})
	require.NoError(t, err)
	require.NoError(t, ctx.Err())
	require.Less(t, len(sink.Watch().Load()), len(anns))
}

// Pins the Anchor-skew limit. A receiver whose Anchor does not yet admit an announcement's signer refuses
// it, and the sender does not send it again on that stream once the Anchor admits the signer. The receiver
// learns it only on a new connection.
func TestValidatorAddrRefusedSignerIsNotResentOnLiveStream(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	lagging, marker := types.GenSecretKey(rng), types.GenSecretKey(rng)
	admitted := utils.NewAtomicSend(false)
	refused := utils.NewAtomicSend(0)
	sink := newVerified(func(ann *types.Signed[types.ValidatorAddrAnnouncement]) error {
		if ann.Key() == lagging.Public() && !admitted.Load() {
			refused.Store(refused.Load() + 1)
			return errors.New("signer not in Anchor")
		}
		return nil
	})
	source := newAddrs(signedAddr(lagging, 1))
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		startStream(ctx, s, source, sink)
		if _, err := refused.Subscribe().Wait(ctx, func(n int) bool { return n > 0 }); err != nil {
			return err
		}
		admitted.Store(true)
		if err := source.Receive(signedAddr(marker, 1)); err != nil {
			return err
		}
		// The stream is ordered: once the marker is stored, any resend of lagging would already have arrived.
		if _, err := sink.Watch().Wait(ctx, func(s testSnapshot) bool { _, ok := s[marker.Public()]; return ok }); err != nil {
			return err
		}
		require.False(t, hasAddr(sink, lagging.Public()))
		startStream(ctx, s, source, sink)
		_, err := sink.Watch().Wait(ctx, func(s testSnapshot) bool { _, ok := s[lagging.Public()]; return ok })
		return err
	})
	require.NoError(t, err)
}

// Announcements flood across every pairing of roles that can connect: a validator dialing a validator,
// a full node dialing a validator, and a full node dialing a full node.
func TestValidatorAddrsReachValidatorsAndFullNodes(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	env := newTestEnv(registry)
	v1, v2 := env.AddNode(t, keys[0]), env.AddNode(t, keys[1])
	validatorAddrs1 := newAddrs(signedAddr(keys[0], 1))
	validatorAddrs2 := newAddrs(signedAddr(keys[1], 1))
	v1.service = NewService(v1.consensus, validatorAddrs1)
	v2.service = NewService(v2.consensus, validatorAddrs2)
	newFullNode := func() (*data.State, *testAddrs, *Service) {
		dataState, err := data.NewState(&data.Config{App: abevm.TestEpochApp(registry)}, blockstore.NewTestStore(t))
		require.NoError(t, err)
		addrs := newAddrs()
		return dataState, addrs, NewFullNodeService(dataState, addrs)
	}
	fullData1, fullAddrs1, full1 := newFullNode()
	fullData2, fullAddrs2, full2 := newFullNode()
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for _, run := range []func(context.Context) error{v1.Run, v2.Run, fullData1.Run, full1.Run, fullData2.Run, full2.Run} {
			s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, run(ctx)) })
		}
		connect := func(listener, dialer *Service, listenerIsCommittee bool) {
			server, client := pipe(ctx, s)
			s.SpawnBg(func() error {
				return utils.IgnoreAfterCancel(ctx, listener.RunServer(ctx, server, listenerIsCommittee))
			})
			s.SpawnBg(func() error {
				return utils.IgnoreAfterCancel(ctx, dialer.RunClient(ctx, client, keys[0].Public(), false))
			})
		}
		connect(v1.service, v2.service, true)
		connect(v1.service, full1, false)
		connect(full1, full2, false)
		for _, addrs := range []*testAddrs{validatorAddrs1, validatorAddrs2, fullAddrs1, fullAddrs2} {
			holdsAll := func(testSnapshot) bool {
				return hasAddr(addrs, keys[0].Public()) && hasAddr(addrs, keys[1].Public())
			}
			if _, err := addrs.Watch().Wait(ctx, holdsAll); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)
}

// A peer that sends announcements the table refuses does not make the stream remember them.
func TestValidatorAddrRefusedAnnouncementsAreNotRemembered(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	member := types.GenSecretKey(rng)
	sink := newVerified(func(ann *types.Signed[types.ValidatorAddrAnnouncement]) error {
		if ann.Key() == member.Public() {
			return nil
		}
		return errors.New("signer not in Anchor")
	})
	peer := newPeerVersions()
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		server, client := pipe(ctx, s)
		s.SpawnBg(func() error {
			return utils.IgnoreAfterCancel(ctx, StreamValidatorAddrs.Serve(ctx, server, func(ctx context.Context, stream validatorAddrStream) error {
				return (&Service{validatorAddrs: sink}).receiveValidatorAddrs(ctx, stream, peer, rate.NewLimiter(validatorAddrRate, validatorAddrBurst))
			}))
		})
		stream, err := StreamValidatorAddrs.Call(ctx, client)
		if err != nil {
			return err
		}
		defer stream.Close()
		for range 50 {
			if err := stream.Send(ctx, types.SignedValidatorAddrAnnouncementConv.Encode(signedAddr(types.GenSecretKey(rng), 1))); err != nil {
				return err
			}
		}
		// The stream is ordered, so once the member is stored every refusal before it has been handled.
		if err := stream.Send(ctx, types.SignedValidatorAddrAnnouncementConv.Encode(signedAddr(member, 1))); err != nil {
			return err
		}
		_, err = sink.Watch().Wait(ctx, func(s testSnapshot) bool { return len(s) > 0 })
		return err
	})
	require.NoError(t, err)
	for versions := range peer.versions.Lock() {
		require.Len(t, versions, 1)
		require.Contains(t, versions, member.Public())
	}
}

// verify runs while writes is held, so Retain cannot run between an announcement's verification and its store.
func TestValidatorAddrsVerifiesUnderWriteLock(t *testing.T) {
	var addrs *ValidatorAddrs[struct{}]
	unlocked := false
	addrs = NewValidatorAddrs(func(*types.Signed[types.ValidatorAddrAnnouncement]) (struct{}, error) {
		if addrs.writes.TryLock() {
			addrs.writes.Unlock()
			unlocked = true
		}
		return struct{}{}, nil
	})
	require.NoError(t, addrs.Receive(signedAddr(types.GenSecretKey(utils.TestRng()), 1)))
	require.False(t, unlocked, "verify ran without writes held")
}

// A resync request reopens the client's stream, so the peer sends its full table again, including
// announcements the client already holds.
func TestValidatorAddrResyncSendsFullTableAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		member := types.GenSecretKey(rng)
		receipts := utils.NewAtomicSend(0)
		sink := newVerified(func(*types.Signed[types.ValidatorAddrAnnouncement]) error {
			receipts.Store(receipts.Load() + 1)
			return nil
		})
		source := newAddrs(signedAddr(member, 1))
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			startStream(ctx, s, source, sink)
			if err := waitAddrs(ctx, sink, 1); err != nil {
				return err
			}
			sink.RequestResync()
			// The second stream waits for the accept pace before the peer sends its table again.
			waitCtx, cancel := context.WithTimeout(ctx, time.Minute)
			defer cancel()
			_, err := receipts.Subscribe().Wait(waitCtx, func(n int) bool { return n >= 2 })
			return err
		})
		require.NoError(t, err)
		require.Equal(t, 2, receipts.Load())
	})
}

// Two peers holding disjoint tables exchange them in both directions. The sink then drops one of the
// source's entries. The live stream never resends it, so only the resync can restore it, and only after
// the accept pace.
func TestValidatorAddrTwoWayResyncRestoresDroppedEntry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		const n = 100
		var aAnns, bAnns []*types.Signed[types.ValidatorAddrAnnouncement]
		for range n {
			aAnns = append(aAnns, signedAddr(types.GenSecretKey(rng), 1))
			bAnns = append(bAnns, signedAddr(types.GenSecretKey(rng), 1))
		}
		srcReceipts := utils.NewAtomicSend(0)
		source := newVerified(func(*types.Signed[types.ValidatorAddrAnnouncement]) error {
			srcReceipts.Store(srcReceipts.Load() + 1)
			return nil
		})
		sinkReceipts := utils.NewAtomicSend(0)
		sink := newVerified(func(*types.Signed[types.ValidatorAddrAnnouncement]) error {
			sinkReceipts.Store(sinkReceipts.Load() + 1)
			return nil
		})
		for _, ann := range aAnns {
			require.NoError(t, source.Receive(ann))
		}
		for _, ann := range bAnns {
			require.NoError(t, sink.Receive(ann))
		}
		// Filling the tables counted receipts; count only the exchange.
		srcReceipts.Store(0)
		sinkReceipts.Store(0)
		dropped := aAnns[0].Key()
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			startStream(ctx, s, source, sink)
			if _, err := sinkReceipts.Subscribe().Wait(ctx, func(k int) bool { return k >= n }); err != nil {
				return err
			}
			if _, err := srcReceipts.Subscribe().Wait(ctx, func(k int) bool { return k >= n }); err != nil {
				return err
			}
			// The tables are disjoint, so each announcement crosses the stream exactly once.
			if sinkReceipts.Load() != n || srcReceipts.Load() != n {
				return fmt.Errorf("round 1 receipts = %d and %d, want %d each", sinkReceipts.Load(), srcReceipts.Load(), n)
			}
			sink.Retain(func(validator types.PublicKey) bool { return validator != dropped })
			if hasAddr(sink, dropped) {
				return fmt.Errorf("sink still holds the dropped entry")
			}
			start := time.Now()
			sink.RequestResync()
			if _, err := sink.Watch().Wait(ctx, func(snapshot testSnapshot) bool {
				_, ok := snapshot[dropped]
				return ok
			}); err != nil {
				return err
			}
			if elapsed := time.Since(start); elapsed < 10*time.Second {
				return fmt.Errorf("resync restored the entry after %v, want at least the accept pace", elapsed)
			}
			return nil
		})
		require.NoError(t, err)
	})
}

// Two resyncs on one connection each deliver the full table. A full table fits in one stream's burst, and
// the second stream is accepted only after the accept pace.
func TestValidatorAddrResyncPacesFullTable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rng := utils.TestRng()
		var anns []*types.Signed[types.ValidatorAddrAnnouncement]
		for range validatorAddrBurst / 2 {
			anns = append(anns, signedAddr(types.GenSecretKey(rng), 1))
		}
		source := newAddrs(anns...)
		receipts := utils.NewAtomicSend(0)
		// The sink keeps nothing, so every announcement it receives is counted. A sink that kept its
		// table would skip echoing it back, and the count would fall short of the table size.
		sink := newVerified(func(*types.Signed[types.ValidatorAddrAnnouncement]) error {
			receipts.Store(receipts.Load() + 1)
			return errors.New("sink keeps nothing")
		})
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		start := time.Now()
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			startStream(ctx, s, source, sink)
			for round := 1; round <= 2; round++ {
				if round > 1 {
					sink.RequestResync()
				}
				if _, err := receipts.Subscribe().Wait(ctx, func(n int) bool { return n >= round*len(anns) }); err != nil {
					return err
				}
			}
			return nil
		})
		require.NoError(t, err)
		require.GreaterOrEqual(t, time.Since(start), 10*time.Second)
	})
}

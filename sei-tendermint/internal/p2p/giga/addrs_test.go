package giga

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
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

// A rejected announcement is dropped without ending the stream: a valid one sent after it still arrives.
func TestValidatorAddrStreamSurvivesRejectedAnnouncement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
	})
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

// A version already held is not verified again, so a peer repeating its table costs no verification.
func TestValidatorAddrsSkipVerifyForHeldVersion(t *testing.T) {
	key := types.GenSecretKey(utils.TestRng())
	verified := 0
	addrs := NewValidatorAddrs(func(*types.Signed[types.ValidatorAddrAnnouncement]) (struct{}, error) {
		verified++
		return struct{}{}, nil
	})
	require.NoError(t, addrs.Receive(signedAddr(key, 1)))
	require.NoError(t, addrs.Receive(signedAddr(key, 1)))
	require.NoError(t, addrs.Receive(signedAddr(key, 1)))
	require.Equal(t, 1, verified)
	require.NoError(t, addrs.Receive(signedAddr(key, 2)))
	require.Equal(t, 2, verified)
}

// An announcement refused on the first push reaches the peer on the next one, once the peer accepts it.
func TestValidatorAddrRefusedSignerIsResentOnNextPush(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		lagging := types.GenSecretKey(utils.TestRng())
		admitted := utils.NewAtomicSend(false)
		sink := newVerified(func(ann *types.Signed[types.ValidatorAddrAnnouncement]) error {
			if ann.Key() == lagging.Public() && !admitted.Load() {
				return errors.New("signer not in Anchor")
			}
			return nil
		})
		source := newAddrs(signedAddr(lagging, 1))
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			startStream(ctx, s, source, sink)
			synctest.Wait()
			require.False(t, hasAddr(sink, lagging.Public()))
			admitted.Store(true)
			start := time.Now()
			if _, err := sink.Watch().Wait(ctx, func(s testSnapshot) bool { _, ok := s[lagging.Public()]; return ok }); err != nil {
				return err
			}
			require.LessOrEqual(t, time.Since(start), validatorAddrInterval)
			return nil
		})
		require.NoError(t, err)
	})
}

// An entry the receiver dropped, and that it had acked, is not resent on the same stream: the sender
// records the ack. It returns once a new connection starts, which sends the full table.
func TestValidatorAddrDroppedAckedEntryReturnsOnReconnect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		dropped, marker, nextMarker := types.GenSecretKey(rng), types.GenSecretKey(rng), types.GenSecretKey(rng)
		source := newAddrs(signedAddr(dropped, 1))
		sink := newAddrs()
		var absentAfterPass bool
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			startStream(ctx, s, source, sink)
			if err := waitAddrs(ctx, sink, 1); err != nil {
				return err
			}
			sink.Retain(func(validator types.PublicKey) bool { return validator != dropped.Public() })
			// Each marker is sent in the next full push, which the stream delivers in order, so the
			// second marker arriving means the push before it has been fully received.
			if err := source.Receive(signedAddr(marker, 1)); err != nil {
				return err
			}
			if _, err := sink.Watch().Wait(ctx, func(s testSnapshot) bool { return hasAddr(sink, marker.Public()) }); err != nil {
				return err
			}
			if err := source.Receive(signedAddr(nextMarker, 1)); err != nil {
				return err
			}
			if _, err := sink.Watch().Wait(ctx, func(s testSnapshot) bool { return hasAddr(sink, nextMarker.Public()) }); err != nil {
				return err
			}
			absentAfterPass = versionOf(sink, dropped.Public()) == 0
			return nil
		})
		require.NoError(t, err)
		require.True(t, absentAfterPass)

		err = scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			startStream(ctx, s, source, sink)
			_, err := sink.Watch().Wait(ctx, func(testSnapshot) bool { return versionOf(sink, dropped.Public()) == 1 })
			return err
		})
		require.NoError(t, err)
	})
}

// A peer that pushes beyond validatorAddrRate has its surplus dropped, and the stream stays open, so
// an announcement sent after the rate has refilled is still received.
func TestValidatorAddrSurplusIsDroppedWithoutEndingStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		var flood []*types.Signed[types.ValidatorAddrAnnouncement]
		for range 2 * validatorAddrBurst {
			flood = append(flood, signedAddr(types.GenSecretKey(rng), 1))
		}
		marker := types.GenSecretKey(rng)
		sink := newAddrs()
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			server, client := pipe(ctx, s)
			// The peer pushes without pacing, then sends the marker after an interval.
			s.SpawnBg(func() error {
				return utils.IgnoreAfterCancel(ctx, StreamValidatorAddrs.Serve(ctx, server, func(ctx context.Context, stream validatorAddrStream) error {
					for _, ann := range flood {
						if err := stream.Send(ctx, encodeAnnouncement(ann)); err != nil {
							return err
						}
					}
					if err := utils.Sleep(ctx, validatorAddrInterval); err != nil {
						return err
					}
					if err := stream.Send(ctx, encodeAnnouncement(signedAddr(marker, 1))); err != nil {
						return err
					}
					<-ctx.Done()
					return nil
				}))
			})
			s.SpawnBg(func() error {
				return utils.IgnoreAfterCancel(ctx, (&Service{validatorAddrs: sink}).clientStreamValidatorAddrs(ctx, client))
			})
			_, err := sink.Watch().Wait(ctx, func(s testSnapshot) bool { _, ok := s[marker.Public()]; return ok })
			return err
		})
		require.NoError(t, err)
		require.Len(t, sink.Watch().Load(), validatorAddrBurst+1)
	})
}

// peerMsg is one message a recording peer received: an announcement, or an ack.
type peerMsg struct {
	validator types.PublicKey
	version   uint64
	ack       bool
}

// recordingPeer serves a stream that sends first, half an interval in, and records every message the
// other side sends it. With ack set, it acks each announcement it receives for the first time. The
// half interval lets the other side's connect push go out before the entries arrive.
func recordingPeer(ctx context.Context, s scope.Scope, ack bool, first ...*types.Signed[types.ValidatorAddrAnnouncement]) (*utils.AtomicSend[[]peerMsg], rpc.Client[API]) {
	return recordingPeerWith(ctx, s, ack, first, nil)
}

// recordingPeerWith is recordingPeer that also sends the messages in extra, in order, after first.
func recordingPeerWith(ctx context.Context, s scope.Scope, ack bool, first []*types.Signed[types.ValidatorAddrAnnouncement], extra []*apb.ValidatorAddrMsg) (*utils.AtomicSend[[]peerMsg], rpc.Client[API]) {
	received := utils.NewAtomicSend([]peerMsg{})
	server, client := pipe(ctx, s)
	s.SpawnBg(func() error {
		return utils.IgnoreAfterCancel(ctx, StreamValidatorAddrs.Serve(ctx, server, func(ctx context.Context, stream validatorAddrStream) error {
			if err := utils.Sleep(ctx, validatorAddrInterval/2); err != nil {
				return err
			}
			seen := map[types.PublicKey]uint64{}
			for _, ann := range first {
				seen[ann.Key()] = max(seen[ann.Key()], ann.Msg().Version)
				if err := stream.Send(ctx, encodeAnnouncement(ann)); err != nil {
					return err
				}
			}
			for _, msg := range extra {
				if err := stream.Send(ctx, msg); err != nil {
					return err
				}
			}
			for {
				raw, err := stream.Recv(ctx)
				if err != nil {
					return err
				}
				switch t := raw.T.(type) {
				case *apb.ValidatorAddrMsg_Announcement:
					ann, err := types.SignedValidatorAddrAnnouncementConv.Decode(t.Announcement)
					if err != nil {
						return err
					}
					received.Store(append(received.Load(), peerMsg{validator: ann.Key(), version: ann.Msg().Version}))
					if fresh := seen[ann.Key()] < ann.Msg().Version; ack && fresh {
						seen[ann.Key()] = ann.Msg().Version
						ackMsg := types.ValidatorAddrAck{Validator: ann.Key(), Version: ann.Msg().Version}
						if err := stream.Send(ctx, encodeAck(ackMsg)); err != nil {
							return err
						}
					}
				case *apb.ValidatorAddrMsg_Ack:
					ack, err := types.ValidatorAddrAckConv.Decode(t.Ack)
					if err != nil {
						return err
					}
					received.Store(append(received.Load(), peerMsg{validator: ack.Validator, version: ack.Version, ack: true}))
				}
			}
		}))
	})
	return &received, client
}

// countMsgs returns how many acks, or announcements, about validator the peer has received.
func countMsgs(received *utils.AtomicSend[[]peerMsg], validator types.PublicKey, ack bool) int {
	n := 0
	for _, msg := range received.Load() {
		if msg.validator == validator && msg.ack == ack {
			n++
		}
	}
	return n
}

// A resent announcement is acked again, so an ack lost on the way reaches the sender on the next push.
func TestValidatorAddrResentAnnouncementIsAckedAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		key := types.GenSecretKey(utils.TestRng())
		sink := newAddrs()
		var received *utils.AtomicSend[[]peerMsg]
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			var client rpc.Client[API]
			received, client = recordingPeerWith(ctx, s, false, nil, []*apb.ValidatorAddrMsg{
				encodeAnnouncement(signedAddr(key, 1)),
				encodeAnnouncement(signedAddr(key, 1)),
			})
			s.SpawnBg(func() error {
				return utils.IgnoreAfterCancel(ctx, (&Service{validatorAddrs: sink}).clientStreamValidatorAddrs(ctx, client))
			})
			_, err := received.Wait(ctx, func(msgs []peerMsg) bool {
				acks := 0
				for _, m := range msgs {
					if m.ack && m.validator == key.Public() && m.version == 1 {
						acks++
					}
				}
				return acks >= 2
			})
			return err
		})
		require.NoError(t, err)
	})
}

// An ack for a validator the source does not yet hold does not suppress that validator's later push.
// The marker follows the ack on the stream, so once the source holds it the ack has been processed.
func TestValidatorAddrAckForUnheldEntryDoesNotSuppressPush(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		unheld, marker := types.GenSecretKey(rng), types.GenSecretKey(rng)
		source := newAddrs()
		var received *utils.AtomicSend[[]peerMsg]
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			var client rpc.Client[API]
			received, client = recordingPeerWith(ctx, s, false, nil, []*apb.ValidatorAddrMsg{
				encodeAck(types.ValidatorAddrAck{Validator: unheld.Public(), Version: 1}),
				encodeAnnouncement(signedAddr(marker, 1)),
			})
			s.SpawnBg(func() error {
				return utils.IgnoreAfterCancel(ctx, (&Service{validatorAddrs: source}).clientStreamValidatorAddrs(ctx, client))
			})
			if err := waitAddrs(ctx, source, 1); err != nil {
				return err
			}
			if err := source.Receive(signedAddr(unheld, 1)); err != nil {
				return err
			}
			_, err := received.Wait(ctx, func(msgs []peerMsg) bool {
				return slices.ContainsFunc(msgs, func(m peerMsg) bool {
					return m.validator == unheld.Public() && !m.ack && m.version == 1
				})
			})
			return err
		})
		require.NoError(t, err)
	})
}

// An ack for a version above the one the source holds does not suppress that version's later push.
// The marker follows the ack on the stream, so once the source holds it the ack has been processed.
func TestValidatorAddrAckAboveHeldVersionDoesNotSuppressNewer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		key, marker := types.GenSecretKey(rng), types.GenSecretKey(rng)
		source := newAddrs(signedAddr(key, 1))
		var received *utils.AtomicSend[[]peerMsg]
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			var client rpc.Client[API]
			received, client = recordingPeerWith(ctx, s, false, nil, []*apb.ValidatorAddrMsg{
				encodeAck(types.ValidatorAddrAck{Validator: key.Public(), Version: 9}),
				encodeAnnouncement(signedAddr(marker, 1)),
			})
			s.SpawnBg(func() error {
				return utils.IgnoreAfterCancel(ctx, (&Service{validatorAddrs: source}).clientStreamValidatorAddrs(ctx, client))
			})
			if err := waitAddrs(ctx, source, 2); err != nil {
				return err
			}
			if err := source.Receive(signedAddr(key, 2)); err != nil {
				return err
			}
			_, err := received.Wait(ctx, func(msgs []peerMsg) bool {
				return slices.ContainsFunc(msgs, func(m peerMsg) bool {
					return m.validator == key.Public() && !m.ack && m.version == 2
				})
			})
			return err
		})
		require.NoError(t, err)
	})
}

// An announcement the peer sent us is acked to it once, and no announcement of it is sent back.
func TestValidatorAddrIsAckedOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		fromPeer, fromElsewhere := types.GenSecretKey(rng), types.GenSecretKey(rng)
		sink := newAddrs(signedAddr(fromElsewhere, 1))
		var received *utils.AtomicSend[[]peerMsg]
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			var client rpc.Client[API]
			received, client = recordingPeer(ctx, s, false, signedAddr(fromPeer, 1))
			s.SpawnBg(func() error {
				return utils.IgnoreAfterCancel(ctx, (&Service{validatorAddrs: sink}).clientStreamValidatorAddrs(ctx, client))
			})
			if err := waitAddrs(ctx, sink, 2); err != nil {
				return err
			}
			return utils.Sleep(ctx, 2*validatorAddrInterval)
		})
		require.NoError(t, err)
		require.Equal(t, 1, countMsgs(received, fromPeer.Public(), true))
		require.Equal(t, 0, countMsgs(received, fromPeer.Public(), false))
		require.GreaterOrEqual(t, countMsgs(received, fromElsewhere.Public(), false), 1)
	})
}

// Once the peer has acked an entry, it is not resent to that peer while the version is unchanged.
func TestValidatorAddrStopsResendingOnceAcked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		key := types.GenSecretKey(utils.TestRng())
		sink := newAddrs(signedAddr(key, 1))
		var received *utils.AtomicSend[[]peerMsg]
		var settled, later int
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			var client rpc.Client[API]
			received, client = recordingPeer(ctx, s, true)
			s.SpawnBg(func() error {
				return utils.IgnoreAfterCancel(ctx, (&Service{validatorAddrs: sink}).clientStreamValidatorAddrs(ctx, client))
			})
			if err := utils.Sleep(ctx, 3*validatorAddrInterval/2); err != nil {
				return err
			}
			settled = countMsgs(received, key.Public(), false)
			if err := utils.Sleep(ctx, 2*validatorAddrInterval); err != nil {
				return err
			}
			later = countMsgs(received, key.Public(), false)
			return nil
		})
		require.NoError(t, err)
		require.NotZero(t, settled)
		require.Equal(t, settled, later)
	})
}

// A newer version of an entry the peer sent us is sent to it, though the older one is not resent.
func TestValidatorAddrNewerVersionIsSentDespiteRecord(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		key := types.GenSecretKey(utils.TestRng())
		sink := newAddrs()
		var received *utils.AtomicSend[[]peerMsg]
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			var client rpc.Client[API]
			received, client = recordingPeer(ctx, s, false, signedAddr(key, 2))
			s.SpawnBg(func() error {
				return utils.IgnoreAfterCancel(ctx, (&Service{validatorAddrs: sink}).clientStreamValidatorAddrs(ctx, client))
			})
			if err := waitAddrs(ctx, sink, 1); err != nil {
				return err
			}
			if err := sink.Receive(signedAddr(key, 3)); err != nil {
				return err
			}
			return utils.Sleep(ctx, 2*validatorAddrInterval)
		})
		require.NoError(t, err)
		var versions []uint64
		for _, msg := range received.Load() {
			if msg.validator == key.Public() && !msg.ack {
				versions = append(versions, msg.version)
			}
		}
		require.Contains(t, versions, uint64(3))
	})
}

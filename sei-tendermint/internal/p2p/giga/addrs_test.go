package giga

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"testing/synctest"

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
func acceptAll(*types.SignedValidatorAddrAnnouncement) bool { return true }

// receiveOK stores ann in a and returns an error if a refuses it.
func receiveOK(a *testAddrs, ann *types.SignedValidatorAddrAnnouncement) error {
	if !a.Receive(ann) {
		return fmt.Errorf("refused announcement of %v", ann.Key())
	}
	return nil
}

// testAddrs is a ValidatorAddrs.
type testAddrs = ValidatorAddrs

// testSnapshot is a snapshot of a testAddrs.
type testSnapshot = ValidatorAddrSnapshot

// newVerified returns a testAddrs that keeps the announcements verify accepts.
func newVerified(verify func(*types.SignedValidatorAddrAnnouncement) bool) *testAddrs {
	return NewValidatorAddrs(func(ann *types.SignedValidatorAddrAnnouncement) bool {
		return verify(ann)
	})
}

// newAddrs returns a ValidatorAddrs that accepts every announcement, already holding anns.
func newAddrs(anns ...*types.SignedValidatorAddrAnnouncement) *testAddrs {
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

func signedAddr(key types.SecretKey, version uint64) *types.SignedValidatorAddrAnnouncement {
	return key.SignValidatorAddr(types.ValidatorAddrAnnouncement{
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
	var anns []*types.SignedValidatorAddrAnnouncement
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
		sink := newVerified(func(ann *types.SignedValidatorAddrAnnouncement) bool {
			if ann.Key() != bad.Public() {
				return true
			}
			rejections.Store(rejections.Load() + 1)
			return false
		})
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			startStream(ctx, s, source, sink)
			if _, err := sink.Watch().Wait(ctx, func(s testSnapshot) bool { return len(s) > 0 }); err != nil {
				return err
			}
			if err := receiveOK(source, signedAddr(bad, 1)); err != nil {
				return err
			}
			if _, err := rejections.Subscribe().Wait(ctx, func(n int) bool { return n > 0 }); err != nil {
				return err
			}
			if err := receiveOK(source, signedAddr(last, 1)); err != nil {
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
	var anns []*types.SignedValidatorAddrAnnouncement
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
	require.NoError(t, receiveOK(source, signedAddr(known, 2)))
	require.NoError(t, receiveOK(source, signedAddr(joiner, 1)))

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
	if ann, ok := a.Watch().Load()[validator]; ok {
		return ann.Msg().Version
	}
	return 0
}

// A stale announcement leaves the held snapshot unchanged.
func TestValidatorAddrsKeepNewestVersion(t *testing.T) {
	rng := utils.TestRng()
	key := types.GenSecretKey(rng)
	addrs := newAddrs()
	require.NoError(t, receiveOK(addrs, signedAddr(key, 2)))
	before := addrs.Watch().Load()
	require.NoError(t, receiveOK(addrs, signedAddr(key, 1)))
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
	var addrs *ValidatorAddrs
	unlocked := false
	addrs = NewValidatorAddrs(func(*types.SignedValidatorAddrAnnouncement) bool {
		if addrs.writes.TryLock() {
			addrs.writes.Unlock()
			unlocked = true
		}
		return true
	})
	require.NoError(t, receiveOK(addrs, signedAddr(types.GenSecretKey(utils.TestRng()), 1)))
	require.False(t, unlocked, "verify ran without writes held")
}

// A version already held is not verified again, so a peer repeating its table costs no verification.
func TestValidatorAddrsSkipVerifyForHeldVersion(t *testing.T) {
	key := types.GenSecretKey(utils.TestRng())
	verified := 0
	addrs := NewValidatorAddrs(func(*types.SignedValidatorAddrAnnouncement) bool {
		verified++
		return true
	})
	require.NoError(t, receiveOK(addrs, signedAddr(key, 1)))
	require.NoError(t, receiveOK(addrs, signedAddr(key, 1)))
	require.NoError(t, receiveOK(addrs, signedAddr(key, 1)))
	require.Equal(t, 1, verified)
	require.NoError(t, receiveOK(addrs, signedAddr(key, 2)))
	require.Equal(t, 2, verified)
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
			if err := receiveOK(source, signedAddr(marker, 1)); err != nil {
				return err
			}
			if _, err := sink.Watch().Wait(ctx, func(s testSnapshot) bool { return hasAddr(sink, marker.Public()) }); err != nil {
				return err
			}
			if err := receiveOK(source, signedAddr(nextMarker, 1)); err != nil {
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

// A message that does not decode, or has no known type, is a protocol violation: the server side
// returns it, which closes the connection.
func TestValidatorAddrViolationEndsServerStream(t *testing.T) {
	nineEndpoints := make([]*apb.ValidatorEndpoint, 9)
	for i := range nineEndpoints {
		nineEndpoints[i] = &apb.ValidatorEndpoint{Name: new("rpc"), HostPort: new("10.0.0.1:8545")}
	}
	for _, tc := range []struct {
		name string
		msg  *apb.ValidatorAddrMsg
	}{
		{name: "unknown type", msg: &apb.ValidatorAddrMsg{}},
		{name: "undecodable ack", msg: &apb.ValidatorAddrMsg{T: &apb.ValidatorAddrMsg_Ack{Ack: &apb.ValidatorAddrAck{}}}},
		{name: "unmarshal failure", msg: &apb.ValidatorAddrMsg{T: &apb.ValidatorAddrMsg_Announcement{Announcement: &apb.SignedValidatorAddrAnnouncement{
			Msg: &apb.ValidatorAddrAnnouncement{Endpoints: nineEndpoints},
		}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := t.Context()
				err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
					server, client := pipe(ctx, s)
					serverErr := utils.NewAtomicSend[error](nil)
					s.SpawnBg(func() error {
						serverErr.Store((&Service{validatorAddrs: newAddrs()}).serverStreamValidatorAddrs(ctx, server))
						return nil
					})
					stream, err := StreamValidatorAddrs.Call(ctx, client)
					if err != nil {
						return err
					}
					if err := stream.Send(ctx, tc.msg); err != nil {
						return err
					}
					got, err := serverErr.Wait(ctx, func(err error) bool { return err != nil })
					if err != nil {
						return err
					}
					require.ErrorIs(t, got, errValidatorAddrViolation)
					return nil
				})
				require.NoError(t, err)
			})
		})
	}
}

// peerMsg is one message a recording peer received: an announcement, or an ack.
type peerMsg struct {
	validator types.PublicKey
	version   uint64
	ack       bool
}

// recordingPeerWith serves a stream that sends first and then the messages in extra, and records every
// message the other side sends it.
func recordingPeerWith(ctx context.Context, s scope.Scope, ack bool, first []*types.SignedValidatorAddrAnnouncement, extra []*apb.ValidatorAddrMsg) (*utils.AtomicSend[[]peerMsg], rpc.Client[API]) {
	return recordingPeerAfterAck(ctx, s, ack, first, extra, nil)
}

// recordingPeerAfterAck is recordingPeerWith that also sends the messages in afterAck once, when the
// first ack arrives. With ack set, it acks each announcement it receives for the first time.
func recordingPeerAfterAck(ctx context.Context, s scope.Scope, ack bool, first []*types.SignedValidatorAddrAnnouncement, extra, afterAck []*apb.ValidatorAddrMsg) (*utils.AtomicSend[[]peerMsg], rpc.Client[API]) {
	received := utils.NewAtomicSend([]peerMsg{})
	server, client := pipe(ctx, s)
	s.SpawnBg(func() error {
		return utils.IgnoreAfterCancel(ctx, StreamValidatorAddrs.Serve(ctx, server, func(ctx context.Context, stream validatorAddrStream) error {
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
			sentAfterAck := false
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
					if !sentAfterAck {
						sentAfterAck = true
						for _, msg := range afterAck {
							if err := stream.Send(ctx, msg); err != nil {
								return err
							}
						}
					}
				}
			}
		}))
	})
	return &received, client
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
			received, client = recordingPeerAfterAck(ctx, s, false, nil, []*apb.ValidatorAddrMsg{
				encodeAnnouncement(signedAddr(key, 1)),
			}, []*apb.ValidatorAddrMsg{
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
			if err := receiveOK(source, signedAddr(unheld, 1)); err != nil {
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
			if err := receiveOK(source, signedAddr(key, 2)); err != nil {
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

// receivedVersions returns the versions of the announcements, or of the acks when ack is set, that the
// peer received about validator.
func receivedVersions(msgs []peerMsg, validator types.PublicKey, ack bool) []uint64 {
	var versions []uint64
	for _, msg := range msgs {
		if msg.validator == validator && msg.ack == ack {
			versions = append(versions, msg.version)
		}
	}
	return versions
}

// An announcement the peer sent us is acked to it once, and is not sent back to it.
func TestValidatorAddrIsAckedOnceNotSentBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		key := types.GenSecretKey(utils.TestRng())
		sink := newAddrs()
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			received, client := recordingPeerWith(ctx, s, true, []*types.SignedValidatorAddrAnnouncement{signedAddr(key, 1)}, nil)
			s.SpawnBg(func() error {
				return utils.IgnoreAfterCancel(ctx, (&Service{validatorAddrs: sink}).clientStreamValidatorAddrs(ctx, client))
			})
			if _, err := received.Wait(ctx, func(msgs []peerMsg) bool { return len(receivedVersions(msgs, key.Public(), true)) == 1 }); err != nil {
				return err
			}
			synctest.Wait()
			msgs := received.Load()
			require.Equal(t, []uint64{1}, receivedVersions(msgs, key.Public(), true))
			require.Empty(t, receivedVersions(msgs, key.Public(), false))
			return nil
		})
		require.NoError(t, err)
	})
}

// A newer version of an entry the peer sent us is sent back to it.
func TestValidatorAddrNewerVersionIsSentDespiteRecord(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		key := types.GenSecretKey(utils.TestRng())
		sink := newAddrs()
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			received, client := recordingPeerWith(ctx, s, false, []*types.SignedValidatorAddrAnnouncement{signedAddr(key, 2)}, nil)
			s.SpawnBg(func() error {
				return utils.IgnoreAfterCancel(ctx, (&Service{validatorAddrs: sink}).clientStreamValidatorAddrs(ctx, client))
			})
			if err := waitAddrs(ctx, sink, 1); err != nil {
				return err
			}
			if err := receiveOK(sink, signedAddr(key, 3)); err != nil {
				return err
			}
			_, err := received.Wait(ctx, func(msgs []peerMsg) bool {
				return slices.Contains(receivedVersions(msgs, key.Public(), false), 3)
			})
			return err
		})
		require.NoError(t, err)
	})
}

// A peer that pushes beyond validatorAddrRate is held back, not cut off: its surplus is received as
// the rate refills, and the stream stays open.
func TestValidatorAddrSurplusIsHeldBackWithoutEndingStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		var flood []*types.SignedValidatorAddrAnnouncement
		for range 2 * validatorAddrBurst {
			flood = append(flood, signedAddr(types.GenSecretKey(rng), 1))
		}
		marker := types.GenSecretKey(rng)
		sink := newAddrs()
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			server, client := pipe(ctx, s)
			s.SpawnBg(func() error {
				return utils.IgnoreAfterCancel(ctx, StreamValidatorAddrs.Serve(ctx, server, func(ctx context.Context, stream validatorAddrStream) error {
					for _, ann := range flood {
						if err := stream.Send(ctx, encodeAnnouncement(ann)); err != nil {
							return err
						}
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
		require.Len(t, sink.Watch().Load(), len(flood)+1)
	})
}

// An announcement the peer has not acked is resent on the next interval.
func TestValidatorAddrUnackedAnnouncementIsResentNextInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		key := types.GenSecretKey(utils.TestRng())
		sink := newAddrs(signedAddr(key, 1))
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			received, client := recordingPeerWith(ctx, s, false, nil, nil)
			s.SpawnBg(func() error {
				return utils.IgnoreAfterCancel(ctx, (&Service{validatorAddrs: sink}).clientStreamValidatorAddrs(ctx, client))
			})
			// The second send can only come from the next interval's full push.
			_, err := received.Wait(ctx, func(msgs []peerMsg) bool {
				return len(receivedVersions(msgs, key.Public(), false)) >= 2
			})
			return err
		})
		require.NoError(t, err)
	})
}

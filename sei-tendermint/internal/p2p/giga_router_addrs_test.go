package p2p

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
)

// testValidatorAddrs returns an empty address store that reads its Anchor from state.
func testValidatorAddrs(state *data.State) *giga.ValidatorAddrs[GigaNodeAddr] {
	return newValidatorAddrs(state)
}

// gossipedAddr returns the dial address of validator that addrs holds, as the dial loop sees it.
func gossipedAddr(addrs *giga.ValidatorAddrs[GigaNodeAddr], validator atypes.PublicKey) (GigaNodeAddr, bool) {
	return (&GigaRouterCommon{cfg: &GigaRouterCommonConfig{}, validatorAddrs: addrs}).validatorAddr(validator)
}

// testAddrAnnouncement returns a valid announcement of key at version, pointing at host:port.
func testAddrAnnouncement(rng utils.Rng, key atypes.SecretKey, version uint64, hostPort string) *atypes.Signed[atypes.ValidatorAddrAnnouncement] {
	return key.Sign(atypes.ValidatorAddrAnnouncement{
		Version:  version,
		NodeKey:  makeKey(rng).Public().String(),
		HostPort: hostPort,
	})
}

// tamperedAddr returns ann with HostPort replaced on the wire. The signature still covers the
// original message, so it no longer verifies against the decoded one.
func tamperedAddr(t *testing.T, ann *atypes.Signed[atypes.ValidatorAddrAnnouncement], hostPort string) *atypes.Signed[atypes.ValidatorAddrAnnouncement] {
	t.Helper()
	raw := atypes.SignedValidatorAddrAnnouncementConv.Encode(ann)
	*raw.Msg.HostPort = hostPort
	out, err := atypes.SignedValidatorAddrAnnouncementConv.Decode(raw)
	require.NoError(t, err)
	return out
}

func TestValidatorAddrVerification(t *testing.T) {
	rng := utils.TestRng()
	member := atypes.GenSecretKey(rng)
	outsider := atypes.GenSecretKey(rng)
	anchor := testAnchor(testEpoch(1, map[atypes.PublicKey]uint64{member.Public(): 1}))
	// signed returns an announcement by key with the default fields, except the one changed by edit.
	signed := func(key atypes.SecretKey, edit func(*atypes.ValidatorAddrAnnouncement)) *atypes.Signed[atypes.ValidatorAddrAnnouncement] {
		msg := atypes.ValidatorAddrAnnouncement{
			Version:  1,
			NodeKey:  makeKey(rng).Public().String(),
			HostPort: "member.example:26656",
		}
		if edit != nil {
			edit(&msg)
		}
		return key.Sign(msg)
	}
	for _, tc := range []struct {
		name    string
		ann     *atypes.Signed[atypes.ValidatorAddrAnnouncement]
		wantErr error
	}{
		{name: "valid", ann: signed(member, nil)},
		{
			name:    "bad signature",
			ann:     tamperedAddr(t, signed(member, nil), "evil.example:26656"),
			wantErr: errAddrBadSignature,
		},
		{name: "signer outside anchor", ann: signed(outsider, nil), wantErr: errAddrSignerNotInAnchor},
		{
			// Membership is checked first, so a non-member's forged announcement is refused without a signature check.
			name:    "forged signature from outside anchor",
			ann:     tamperedAddr(t, signed(outsider, nil), "evil.example:26656"),
			wantErr: errAddrSignerNotInAnchor,
		},
		{
			name:    "malformed node key",
			ann:     signed(member, func(m *atypes.ValidatorAddrAnnouncement) { m.NodeKey = "not-a-node-key" }),
			wantErr: errAddrBadNodeKey,
		},
		{
			name:    "malformed host port",
			ann:     signed(member, func(m *atypes.ValidatorAddrAnnouncement) { m.HostPort = "no-port" }),
			wantErr: errAddrBadHostPort,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := verifyValidatorAddr(anchor, tc.ann)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// An Anchor admits the members of both its epochs: a validator entering with Epochs[1] verifies
// like one in Epochs[0], and a key in neither is refused.
func TestValidatorAddrVerificationAcceptsEitherAnchorEpoch(t *testing.T) {
	rng := utils.TestRng()
	current := atypes.GenSecretKey(rng)
	joiner := atypes.GenSecretKey(rng)
	outsider := atypes.GenSecretKey(rng)
	anchor := data.Anchor{Epochs: [2]*atypes.Epoch{
		testEpoch(1, map[atypes.PublicKey]uint64{current.Public(): 1}),
		testEpoch(2, map[atypes.PublicKey]uint64{joiner.Public(): 1}),
	}}
	signed := func(key atypes.SecretKey) *atypes.Signed[atypes.ValidatorAddrAnnouncement] {
		return key.Sign(atypes.ValidatorAddrAnnouncement{
			Version:  1,
			NodeKey:  makeKey(rng).Public().String(),
			HostPort: "member.example:26656",
		})
	}
	_, err := verifyValidatorAddr(anchor, signed(current))
	require.NoError(t, err)
	_, err = verifyValidatorAddr(anchor, signed(joiner))
	require.NoError(t, err)
	_, err = verifyValidatorAddr(anchor, signed(outsider))
	require.ErrorIs(t, err, errAddrSignerNotInAnchor)
}

func TestValidatorAddrBookTakesPrecedence(t *testing.T) {
	rng := utils.TestRng()
	member := atypes.GenSecretKey(rng)
	epochs := newSettledEpochs(t, testEpoch(1, map[atypes.PublicKey]uint64{member.Public(): 1}))
	table := testValidatorAddrs(epochs.state)
	require.NoError(t, table.Receive(testAddrAnnouncement(rng, member, 1, "gossip.example:26656")))
	book := GigaNodeAddr{Key: makeKey(rng).Public(), HostPort: tcp.HostPort{Hostname: "book.example", Port: 26656}}
	router := &GigaRouterCommon{
		cfg:            &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{member.Public(): book}},
		validatorAddrs: table,
	}
	got, ok := router.validatorAddr(member.Public())
	require.True(t, ok)
	require.NoError(t, utils.TestDiff(book, got))
}

// An announcement refused before the Anchor admits its signer is learned when it is received again.
func TestValidatorAddrRefusedBeforeAnchorAdmissionIsLearnedWhenReceivedAgain(t *testing.T) {
	rng := utils.TestRng()
	stay := atypes.GenSecretKey(rng)
	joiner := atypes.GenSecretKey(rng)
	epochs := newSettledEpochs(t, testEpoch(1, map[atypes.PublicKey]uint64{stay.Public(): 1}))
	addrs := testValidatorAddrs(epochs.state)
	ann := testAddrAnnouncement(rng, joiner, 1, "joiner.example:26656")
	require.ErrorIs(t, addrs.Receive(ann), errAddrSignerNotInAnchor)
	_, ok := gossipedAddr(addrs, joiner.Public())
	require.False(t, ok)

	epochs.store(testEpoch(2, map[atypes.PublicKey]uint64{stay.Public(): 1, joiner.Public(): 1}))
	require.NoError(t, addrs.Receive(ann))
	addr, ok := gossipedAddr(addrs, joiner.Public())
	require.True(t, ok)
	require.Equal(t, "joiner.example", addr.HostPort.Hostname)
}

// A member whose address arrives by gossip is dialed at that address. When the member leaves
// the committee its session stops and its gossiped entry is dropped.
func TestGigaRouterCommon_RunPerCommitteeMemberDialsGossipedMember(t *testing.T) {
	rng := utils.TestRng()
	keyA := atypes.GenSecretKey(rng)
	keyB := atypes.GenSecretKey(rng)
	a, b := keyA.Public(), keyB.Public()
	epochs := newSettledEpochs(t, testEpoch(1, map[atypes.PublicKey]uint64{a: 1, b: 1}))
	validatorAddrs := testValidatorAddrs(epochs.state)
	router := &GigaRouterCommon{cfg: &GigaRouterCommonConfig{}, data: epochs.state, validatorAddrs: validatorAddrs}
	dialed := make(chan GigaNodeAddr, 4)
	stopped := make(chan struct{}, 4)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(router.runPerCommitteeMember(ctx, false, func(ctx context.Context, validator atypes.PublicKey, addr GigaNodeAddr) error {
				if validator == b {
					dialed <- addr
					<-ctx.Done()
					stopped <- struct{}{}
				}
				return ctx.Err()
			}))
		})
		require.NoError(t, validatorAddrs.Receive(testAddrAnnouncement(rng, keyB, 1, "b.example:26656")))
		addr := <-dialed
		require.Equal(t, "b.example", addr.HostPort.Hostname)
		require.Equal(t, uint16(26656), addr.HostPort.Port)

		epochs.store(testEpoch(2, map[atypes.PublicKey]uint64{a: 1}))
		<-stopped
		// The entry is dropped after the session stops.
		_, err := validatorAddrs.Watch().Wait(ctx, func(held giga.ValidatorAddrSnapshot[GigaNodeAddr]) bool {
			_, ok := held[b]
			return !ok
		})
		return err
	})
	require.NoError(t, err)
}

// A member whose gossiped address changes is stopped and redialed at the new address.
func TestGigaRouterCommon_RunPerCommitteeMemberRedialsChangedAddress(t *testing.T) {
	rng := utils.TestRng()
	keyB := atypes.GenSecretKey(rng)
	b := keyB.Public()
	epochs := newSettledEpochs(t, testEpoch(1, map[atypes.PublicKey]uint64{b: 1}))
	validatorAddrs := testValidatorAddrs(epochs.state)
	router := &GigaRouterCommon{cfg: &GigaRouterCommonConfig{}, data: epochs.state, validatorAddrs: validatorAddrs}
	dialed := make(chan GigaNodeAddr, 4)
	stopped := make(chan struct{}, 4)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(router.runPerCommitteeMember(ctx, false, func(ctx context.Context, validator atypes.PublicKey, addr GigaNodeAddr) error {
				dialed <- addr
				<-ctx.Done()
				stopped <- struct{}{}
				return ctx.Err()
			}))
		})
		require.NoError(t, validatorAddrs.Receive(testAddrAnnouncement(rng, keyB, 1, "b1.example:26656")))
		require.Equal(t, "b1.example", (<-dialed).HostPort.Hostname)

		require.NoError(t, validatorAddrs.Receive(testAddrAnnouncement(rng, keyB, 2, "b2.example:26656")))
		<-stopped
		require.Equal(t, "b2.example", (<-dialed).HostPort.Hostname)
		return nil
	})
	require.NoError(t, err)
}

// testPublishingRouter returns a validator router that publishes key's announcement of nodeKey at hp into table.
func testPublishingRouter(t *testing.T, state *data.State, table *giga.ValidatorAddrs[GigaNodeAddr], key atypes.SecretKey, nodeKey NodeSecretKey, hp tcp.HostPort) *gigaValidatorRouter {
	t.Helper()
	return &gigaValidatorRouter{
		GigaRouterCommon: &GigaRouterCommon{
			data:           state,
			validatorAddrs: table,
			key:            nodeKey,
			validatorKey:   utils.Some(key.Public()),
			selfAddr:       utils.Some(NodeAddress{NodeID: nodeKey.Public().NodeID(), Hostname: hp.Hostname, Port: hp.Port}),
			gigaSpec:       gigaHandshakeSpec{ValidatorKey: utils.Some(key), EVMRPC: testEVMRPCURL()},
		},
	}
}

// A validator's own announcement is verified like a peer's: one that peers would drop is refused, not stored.
func TestGigaValidatorRouter_PublishValidatorAddrIsVerified(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	epochs := newSettledEpochs(t, testEpoch(1, map[atypes.PublicKey]uint64{key.Public(): 1}))
	nodeKey := makeKey(rng)
	hp := tcp.HostPort{Hostname: "self.example", Port: 26656}

	table := testValidatorAddrs(epochs.state)
	router := testPublishingRouter(t, epochs.state, table, key, nodeKey, hp)
	require.NoError(t, router.publishValidatorAddr(1))
	addr, ok := gossipedAddr(table, key.Public())
	require.True(t, ok)
	require.NoError(t, utils.TestDiff(GigaNodeAddr{Key: nodeKey.Public(), HostPort: hp}, addr))

	// A key outside every Anchor epoch is refused, as it would be by any peer's table.
	outsider := atypes.GenSecretKey(rng)
	table = testValidatorAddrs(epochs.state)
	router = testPublishingRouter(t, epochs.state, table, outsider, nodeKey, hp)
	require.ErrorIs(t, router.publishValidatorAddr(1), errAddrSignerNotInAnchor)
	_, ok = gossipedAddr(table, outsider.Public())
	require.False(t, ok)
}

// A validator outside every Anchor epoch publishes nothing until it enters one.
func TestGigaValidatorRouter_RunPublishValidatorAddrWaitsForAnchorMembership(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	other := atypes.GenSecretKey(rng).Public()
	epochs := newSettledEpochs(t, testEpoch(1, map[atypes.PublicKey]uint64{other: 1}))
	table := testValidatorAddrs(epochs.state)
	nodeKey := makeKey(rng)
	hp := tcp.HostPort{Hostname: "self.example", Port: 26656}
	router := testPublishingRouter(t, epochs.state, table, key, nodeKey, hp)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(router.runPublishValidatorAddr(ctx)) })
		epochs.store(testEpoch(2, map[atypes.PublicKey]uint64{other: 1, key.Public(): 1}))
		_, err := table.Watch().Wait(ctx, func(held giga.ValidatorAddrSnapshot[GigaNodeAddr]) bool { return len(held) > 0 })
		return err
	})
	require.NoError(t, err)
	addr, ok := gossipedAddr(table, key.Public())
	require.True(t, ok)
	require.NoError(t, utils.TestDiff(GigaNodeAddr{Key: nodeKey.Public(), HostPort: hp}, addr))
}

// A validator that leaves an Anchor and enters again publishes a higher version the second time.
// A peer that sends back a higher version of this validator's own announcement, as after a restart,
// makes the validator publish again above it.
func TestGigaValidatorRouter_RepublishesAboveHigherOwnVersion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rng := utils.TestRng()
		key := atypes.GenSecretKey(rng)
		epochs := newSettledEpochs(t, testEpoch(1, map[atypes.PublicKey]uint64{key.Public(): 1}))
		table := testValidatorAddrs(epochs.state)
		router := testPublishingRouter(t, epochs.state, table, key, makeKey(rng), tcp.HostPort{Hostname: "self.example", Port: 26656})
		const stale = uint64(1) << 62
		err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
			s.SpawnBg(func() error { return utils.IgnoreCancel(router.runPublishValidatorAddr(ctx)) })
			synctest.Wait()
			if err := table.Receive(testAddrAnnouncement(rng, key, stale, "old.example:26656")); err != nil {
				return err
			}
			synctest.Wait()
			entry, ok := table.Watch().Load()[key.Public()]
			if !ok || entry.Ann.Msg().Version <= stale {
				return fmt.Errorf("own version after republish = %d, %v; want above %d", entry.Ann.Msg().Version, ok, stale)
			}
			addr, ok := table.Get(key.Public())
			if !ok || addr.HostPort.Hostname != "self.example" {
				return fmt.Errorf("own address after republish = %v, %v; want self.example", addr.HostPort, ok)
			}
			return nil
		})
		require.NoError(t, err)
	})
}

func TestGigaValidatorRouter_RunPublishValidatorAddrVersionRisesOnReentry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rng := utils.TestRng()
		key := atypes.GenSecretKey(rng)
		other := atypes.GenSecretKey(rng).Public()
		epochs := newSettledEpochs(t, testEpoch(1, map[atypes.PublicKey]uint64{key.Public(): 1}))
		table := testValidatorAddrs(epochs.state)
		router := testPublishingRouter(t, epochs.state, table, key, makeKey(rng), tcp.HostPort{Hostname: "self.example", Port: 26656})
		heldVersion := func() (uint64, bool) {
			entry, ok := table.Watch().Load()[key.Public()]
			return entry.Ann.Msg().Version, ok
		}
		err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
			s.SpawnBg(func() error { return utils.IgnoreCancel(router.runPublishValidatorAddr(ctx)) })
			// synctest.Wait returns once the router is durably blocked, so each step below is observed in order.
			synctest.Wait()
			first, ok := heldVersion()
			if !ok {
				return errors.New("no announcement published on entry")
			}
			epochs.store(testEpoch(2, map[atypes.PublicKey]uint64{other: 1}))
			synctest.Wait()
			epochs.store(testEpoch(3, map[atypes.PublicKey]uint64{key.Public(): 1}))
			synctest.Wait()
			second, ok := heldVersion()
			if !ok || second <= first {
				return fmt.Errorf("version after re-entry = %d, %v; want above %d", second, ok, first)
			}
			return nil
		})
		require.NoError(t, err)
	})
}

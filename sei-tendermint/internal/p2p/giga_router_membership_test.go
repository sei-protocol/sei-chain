package p2p

import (
	"context"
	"fmt"
	"testing"
	"time"

	ethrpc "github.com/ethereum/go-ethereum/rpc"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
)

// joinTestNode is a committee member's router, serving and dialing over real TCP.
type joinTestNode struct {
	cfg      *testNodeCfg
	listener *tcp.Listener
	epochs   *settledEpochs
	addrs    *giga.ValidatorAddrs
	router   *GigaRouterCommon
}

// newJoinTestNode returns a member node whose Anchor starts at ep and whose configured book is book.
// The node is not running and has not announced its address.
func newJoinTestNode(t *testing.T, cfg *testNodeCfg, ep *atypes.Epoch, book map[atypes.PublicKey]GigaNodeAddr) *joinTestNode {
	t.Helper()
	// The listener is closed by the serve loop when the node's context ends.
	listener, err := tcp.Listen(cfg.addr)
	require.NoError(t, err)
	epochs := newSettledEpochs(t, ep)
	addrs := newValidatorAddrs(epochs.state, true)
	return &joinTestNode{
		cfg:      cfg,
		listener: listener,
		epochs:   epochs,
		addrs:    addrs,
		router: &GigaRouterCommon{
			cfg: &GigaRouterCommonConfig{
				DialInterval:    100 * time.Millisecond,
				AllowLocalAddrs: true,
				ValidatorAddrs:  book,
			},
			key:                  cfg.nodeKey,
			data:                 epochs.state,
			service:              giga.NewFullNodeService(epochs.state, addrs),
			poolIn:               giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
			poolInCommittee:      giga.NewPool[atypes.PublicKey, rpc.Server[giga.API]](),
			poolOut:              giga.NewPool[atypes.PublicKey, rpc.Client[giga.API]](),
			proxies:              utils.NewRWMutex(map[atypes.PublicKey]*ethrpc.Client{}),
			gigaSpec:             gigaHandshakeSpec{ValidatorKey: utils.Some(cfg.validatorKey), EVMRPC: testEVMRPCURL()},
			validatorAddrs:       addrs,
			inboundFullnodeCount: utils.NewMutex(new(int64)),
		},
	}
}

// announce receives this node's own address announcement, which gossip then carries to its peers.
func (n *joinTestNode) announce() error {
	ann := n.cfg.validatorKey.SignValidatorAddr(atypes.ValidatorAddrAnnouncement{
		Version:  1,
		NodeKey:  n.cfg.nodeKey.Public().String(),
		HostPort: tcp.HostPort{Hostname: n.cfg.addr.Addr().String(), Port: n.cfg.addr.Port()}.String(),
	})
	if !n.addrs.Receive(ann) {
		return fmt.Errorf("table refused own announcement %v", ann.Key())
	}
	return nil
}

// run serves inbound connections and dials every committee member it can address, until ctx ends.
func (n *joinTestNode) run(ctx context.Context, s scope.Scope) {
	s.SpawnBg(func() error { return utils.IgnoreCancel(n.serve(ctx, s)) })
	s.SpawnBg(func() error {
		return utils.IgnoreCancel(n.router.runPerCommitteeMember(ctx, false, n.dialMember))
	})
}

func (n *joinTestNode) serve(ctx context.Context, s scope.Scope) error {
	for {
		c, err := n.listener.AcceptOrClose(ctx)
		if err != nil {
			return err
		}
		s.SpawnBg(func() error {
			// A refused or closed connection is expected here, so it does not end the node.
			if err := n.serveConn(ctx, c); err != nil {
				logger.Info("test member inbound connection ended", "node", n.cfg.nodeKey.Public(), "err", err)
			}
			return nil
		})
	}
}

func (n *joinTestNode) serveConn(ctx context.Context, c tcp.Conn) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return c.Run(ctx) })
		hConn, err := handshake(ctx, c, handshakeSpec{
			NodeKey:  n.cfg.nodeKey,
			IsServer: true,
			Giga:     utils.Some(n.router.gigaSpec),
		})
		if err != nil {
			return err
		}
		return n.router.runInboundConn(ctx, hConn)
	})
}

// dialMember keeps an outbound connection to validator at addr. A dial that ends is retried only once
// the node's Anchor or address table has changed since the attempt began.
func (n *joinTestNode) dialMember(ctx context.Context, validator atypes.PublicKey, addr GigaNodeAddr) error {
	for {
		anchor := n.epochs.state.Anchor().Load()
		snapshot := n.addrs.Watch().Load()
		err := n.router.dialAndRunConn(ctx, validator, addr.Key, addr.HostPort, func(ctx context.Context, client rpc.Client[giga.API]) error {
			return n.router.service.RunClient(ctx, client, validator, true)
		})
		logger.Info("test member outbound connection ended", "node", n.cfg.nodeKey.Public(), "validator", validator, "err", err)
		if err := n.waitChanged(ctx, anchor, snapshot); err != nil {
			return err
		}
	}
}

// waitChanged returns once the node's Anchor or address table differs from anchor or snapshot. It
// checks before waiting, so a change made during the attempt is not missed.
func (n *joinTestNode) waitChanged(ctx context.Context, anchor data.Anchor, snapshot giga.ValidatorAddrSnapshot) error {
	return utils.WaitAny(ctx, func() bool {
		return n.epochs.state.Anchor().Load().Epochs != anchor.Epochs || !n.addrs.Watch().Load().Same(snapshot)
	}, n.epochs.state.Anchor(), n.addrs.Watch())
}

// waitHeld waits until the node's address table holds validator, or no longer holds it when held is false.
func (n *joinTestNode) waitHeld(ctx context.Context, validator atypes.PublicKey, held bool) error {
	_, err := n.addrs.Watch().Wait(ctx, func(s giga.ValidatorAddrSnapshot) bool {
		_, ok := s[validator]
		return ok == held
	})
	return err
}

// expectConnected returns an error unless the node has an outbound connection to validator.
func (n *joinTestNode) expectConnected(validator atypes.PublicKey, want bool) error {
	if _, ok := n.router.poolOut.Get(validator); ok != want {
		return fmt.Errorf("outbound connection to %v = %v, want %v", validator, ok, want)
	}
	return nil
}

// TestGigaRouter_MemberJoinsAndLeavesOverTCP runs three committee members over real TCP and
// moves the Anchor twice: C joins, then B leaves. The seed A is the only address C is configured
// with, so C reaches B through gossip; A and C must end with no connection or table entry for B.
func TestGigaRouter_MemberJoinsAndLeavesOverTCP(t *testing.T) {
	rng := utils.TestRng()
	cfgA := &testNodeCfg{validatorKey: atypes.GenSecretKey(rng), nodeKey: makeKey(rng), addr: tcp.TestReserveAddr()}
	cfgB := &testNodeCfg{validatorKey: atypes.GenSecretKey(rng), nodeKey: makeKey(rng), addr: tcp.TestReserveAddr()}
	cfgC := &testNodeCfg{validatorKey: atypes.GenSecretKey(rng), nodeKey: makeKey(rng), addr: tcp.TestReserveAddr()}
	a, b, c := cfgA.validatorKey.Public(), cfgB.validatorKey.Public(), cfgC.validatorKey.Public()
	// Epochs hold one road each, so every Anchor move is an epoch boundary.
	ep2 := testEpoch(2, map[atypes.PublicKey]uint64{a: 1, b: 1})
	ep3 := testEpoch(3, map[atypes.PublicKey]uint64{a: 1, b: 1, c: 1})
	ep4 := testEpoch(4, map[atypes.PublicKey]uint64{a: 1, c: 1})
	nodeA := newJoinTestNode(t, cfgA, ep2, map[atypes.PublicKey]GigaNodeAddr{})
	nodeB := newJoinTestNode(t, cfgB, ep2, map[atypes.PublicKey]GigaNodeAddr{a: cfgA.GigaNodeAddr()})
	// C is configured with the seed alone, and is not a member until the join.
	nodeC := newJoinTestNode(t, cfgC, ep2, map[atypes.PublicKey]GigaNodeAddr{a: cfgA.GigaNodeAddr()})

	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		// Before the join, A and B reach each other: B dials A from its book, and A learns B from B's announcement.
		for _, n := range []*joinTestNode{nodeA, nodeB} {
			if err := n.announce(); err != nil {
				return err
			}
			n.run(ctx, s)
		}
		if err := nodeA.router.poolOut.WaitFor(ctx, b); err != nil {
			return err
		}
		if err := nodeB.router.poolOut.WaitFor(ctx, a); err != nil {
			return err
		}

		// Join: C becomes a member on every node, then starts.
		for _, n := range []*joinTestNode{nodeA, nodeB, nodeC} {
			n.epochs.store(ep3)
		}
		if err := nodeC.announce(); err != nil {
			return err
		}
		nodeC.run(ctx, s)
		// C was configured with A only, so it learns B's address through A's gossip.
		if err := nodeC.waitHeld(ctx, b, true); err != nil {
			return err
		}
		if err := nodeC.router.poolOut.WaitFor(ctx, b); err != nil {
			return err
		}
		if err := nodeC.router.poolOut.WaitFor(ctx, a); err != nil {
			return err
		}
		// Everyone connects to C, including the member that was not its seed.
		if err := nodeA.router.poolOut.WaitFor(ctx, c); err != nil {
			return err
		}
		if err := nodeB.router.poolOut.WaitFor(ctx, c); err != nil {
			return err
		}

		// Leave: B is removed from the Anchor on every node, B included.
		for _, n := range []*joinTestNode{nodeA, nodeB, nodeC} {
			n.epochs.store(ep4)
		}
		// A and C drop B's entry, and the sessions to B are stopped before that entry is dropped.
		for _, n := range []*joinTestNode{nodeA, nodeC} {
			if err := n.waitHeld(ctx, b, false); err != nil {
				return err
			}
			if err := n.expectConnected(b, false); err != nil {
				return fmt.Errorf("after leave: %w", err)
			}
		}
		// The members that stay keep their connections to each other.
		if err := nodeA.expectConnected(c, true); err != nil {
			return err
		}
		if err := nodeC.expectConnected(a, true); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)
}

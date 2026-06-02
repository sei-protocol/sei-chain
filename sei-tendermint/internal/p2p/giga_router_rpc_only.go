package p2p

import (
	"context"
	"fmt"
	"maps"
	"math/rand/v2"
	"net/url"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// gigaRPCOnlyRouter is the GigaRouter impl for non-validator nodes. It
// pulls finalized blocks from committee members via the giga block-sync
// subscriber and executes them locally; it does not run consensus or
// producer, does not accept inbound giga connections, and sources its
// "last committed block" from data.State directly.
type gigaRPCOnlyRouter struct {
	*gigaRouterCommon
}

// IsRPCOnly reports whether this router was constructed in rpc-only
// (non-validator) mode. Rpc-only nodes pull finalized blocks from
// committee members and execute them locally; they don't produce blocks
// or participate in consensus voting.
func (r *gigaRPCOnlyRouter) IsRPCOnly() bool { return true }

// LastCommittedBlockNumber returns the highest global block number finalized
// by consensus. Rpc-only nodes read from data.State (NextBlock-1), which
// tracks blocks durably pushed via the giga block-sync subscriber. Safe
// for high-frequency callers — the path is lock-free.
func (r *gigaRPCOnlyRouter) LastCommittedBlockNumber() int64 {
	// data.State.NextBlock returns the next height to push. The most
	// recently pushed block is NextBlock - 1; before any block has landed
	// NextBlock equals the genesis InitialHeight so this returns
	// InitialHeight - 1 (0 for a fresh genesis-at-1 chain).
	return int64(r.data.NextBlock()) - 1 // nolint:gosec // bounded by actual chain height.
}

// MaxGasPerBlock returns the max gas per block from genesis. Rpc-only nodes
// don't build a producer.Config; they source the same value the validator
// path stores in producer.Config from genesis instead (see buildGigaConfig
// in node/setup.go for the validator side of the populate).
func (r *gigaRPCOnlyRouter) MaxGasPerBlock() int64 {
	if r.cfg.GenDoc.ConsensusParams != nil {
		return r.cfg.GenDoc.ConsensusParams.Block.MaxGas
	}
	return 0
}

func (r *gigaRPCOnlyRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// Rpc-only: stick to one validator at a time. Walk through the
		// committee in a stable order; on disconnect, move to the
		// next. Avoids the N× QC duplication of dialing every
		// committee member.
		//
		// TODO(autobahn-rpc-only): allow hard-configuring a preferred
		// validator (or a subset of trusted validators) instead of
		// walking the whole committee.
		s.Spawn(func() error { return r.runRPCOnlySubscriber(ctx) })
		s.SpawnNamed("data", func() error { return r.data.Run(ctx) })
		s.SpawnNamed("execute", func() error { return r.runExecute(ctx) })
		s.SpawnNamed("service", func() error { return r.service.Run(ctx) })
		return nil
	})
}

func (r *gigaRPCOnlyRouter) RunInboundConn(ctx context.Context, hConn *handshakedConn) error {
	// Rpc-only nodes only dial outbound to committee members for block
	// sync; they don't accept inbound peers (no poolIn, no inbound
	// service handlers). Reject at the door.
	return fmt.Errorf("rpc-only node does not accept inbound giga connections")
}

func (r *gigaRPCOnlyRouter) EvmProxy(sender common.Address) (*url.URL, bool) {
	shardValidator := r.data.Committee().EvmShard(sender)
	// Rpc-only nodes have no validator key, so the shard owner is never
	// "us" — always forward.
	return r.cfg.ValidatorAddrs[shardValidator].EVMRPC.Get()
}

// rpcOnlyHealthyConnDuration is how long a dialed connection must stay up
// before we treat it as a successful subscription (used to reset the
// failover backoff). Below this, we count it as "dialed and immediately
// dropped" — same shape as a rejection.
const rpcOnlyHealthyConnDuration = 30 * time.Second

// rpcOnlyMaxBackoff caps the inter-cycle backoff. Persistent failure
// (all validators down, or all at their inbound rpc-only cap) is an
// operator-fixable condition that doesn't justify aborting the process,
// so we stop doubling once the wait reaches this value and just keep
// retrying at that rate.
const rpcOnlyMaxBackoff = 5 * time.Minute

// runRPCOnlySubscriber implements the rpc-only single-active-subscriber
// dial loop: pick a committee member, dial + run block sync against it,
// advance to the next on disconnect or rejection. Loops forever; exits
// only when ctx is cancelled.
//
// The committee list is shuffled once at startup so multiple rpc-only
// nodes don't all converge on the same first-choice validator (which
// would imbalance load across the committee). Each node's starting
// preference is independently random; failover preserves that order.
//
// Backoff: between attempts within a cycle we sleep cfg.DialInterval
// (matches the validator side). After a full pass with no healthy
// connection we double the inter-attempt sleep, capped at
// rpcOnlyMaxBackoff so a persistently-saturated cluster (e.g. all
// validators at their inbound rpc-only cap) keeps retrying at a polite
// rate instead of unbounded. A single attempt that runs longer than
// rpcOnlyHealthyConnDuration is treated as a successful subscription and
// resets the backoff to cfg.DialInterval.
//
// TODO(autobahn-state-sync): block sync from a single peer is bounded by
// that peer's per-stream rate limit (GetBlock's rpc.Limit{Rate:10,
// Concurrent:10}), which makes initial catch-up of a node joining an
// established cluster slow — minutes per few thousand blocks. The
// long-term fix is autobahn snapshot transfer (CometBFT-style state
// sync), letting a fresh rpc-only jump to recent state instead of
// replaying from genesis. This loop is correct for "fresh node on a
// fresh cluster" and "restart of a near-tip node" — the two cases
// production rpc-only nodes hit once state sync lands.
func (r *gigaRPCOnlyRouter) runRPCOnlySubscriber(ctx context.Context) error {
	addrs := slices.Collect(maps.Values(r.cfg.ValidatorAddrs))
	rand.Shuffle(len(addrs), func(i, j int) { addrs[i], addrs[j] = addrs[j], addrs[i] })
	backoff := r.cfg.DialInterval
	failsThisCycle := 0
	// lastHealthyOrStart marks the most recent healthy connection (or the
	// loop's start, if no connection has been healthy yet). Used to gate
	// backoff escalation: a recent healthy run shouldn't trigger doubling
	// just because a short burst of failures follows it.
	lastHealthyOrStart := time.Now()
	for i := 0; ; i = (i + 1) % len(addrs) {
		addr := addrs[i]
		start := time.Now()
		err := r.dialAndRunConn(ctx, addr.Key, addr.HostPort, r.service.RunBlockSyncClient)
		if time.Since(start) >= rpcOnlyHealthyConnDuration {
			// Connection ran long enough to count as accepted; reset.
			backoff = r.cfg.DialInterval
			failsThisCycle = 0
			lastHealthyOrStart = time.Now()
		} else {
			failsThisCycle++
		}
		logger.Info("rpc-only giga connection ended; failing over",
			"addr", addr, "err", err, "backoff", backoff)
		if err := utils.Sleep(ctx, backoff); err != nil {
			return err
		}
		// Completed a full pass without a healthy connection: escalate
		// backoff only if it has actually been a while since the last
		// healthy run. Otherwise the cluster might just be momentarily
		// saturated post-reconnect; reset the counter and try again at
		// the current backoff.
		if failsThisCycle >= len(addrs) {
			if time.Since(lastHealthyOrStart) >= rpcOnlyHealthyConnDuration {
				backoff = min(backoff*2, rpcOnlyMaxBackoff)
			}
			failsThisCycle = 0
		}
	}
}

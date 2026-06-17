package p2p

import (
	"context"
	"maps"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// gigaFullnodeRouter is the GigaRouter impl for non-validator nodes. It
// pulls finalized blocks from committee members via the giga block-sync
// subscriber and executes them locally; it does not run consensus or
// producer, does not accept inbound giga connections, and sources its
// "last committed block" from data.State directly.
type gigaFullnodeRouter struct {
	*gigaRouterCommon
}

// MaxGasEstimatedPerBlock returns the chain's per-block gas limit from
// genesis (consensus_params.block.max_gas). Fullnodes don't build a
// producer.Config; they source the same value the validator path stores
// in producer.Config.MaxGasEstimatedPerBlock from genesis instead (see
// buildValidatorGigaConfig in node/setup.go for the validator side).
func (r *gigaFullnodeRouter) MaxGasEstimatedPerBlock() uint64 {
	if r.cfg.GenDoc.ConsensusParams != nil {
		return r.cfg.GenDoc.ConsensusParams.Block.MaxGasUint64()
	}
	return 0
}

// AsValidator returns None — fullnodes don't expose the validator-only
// mempool surface. Every EVM tx is forwarded to the shard owner via
// EvmProxy, not inserted locally.
func (r *gigaFullnodeRouter) AsValidator() utils.Option[GigaValidatorRouter] {
	return utils.None[GigaValidatorRouter]()
}

func (r *gigaFullnodeRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// Fullnode: stick to one validator at a time. Walk through the
		// committee in a stable order; on disconnect, move to the
		// next. Avoids the N× QC duplication of dialing every
		// committee member.
		//
		// TODO(autobahn-fullnode): allow hard-configuring a preferred
		// validator (or a subset of trusted validators) instead of
		// walking the whole committee.
		s.Spawn(func() error { return r.runFullnodeSubscriber(ctx) })
		r.spawnReadPath(ctx, s)
		return nil
	})
}

// fullnodeHealthyConnDuration is how long a dialed connection must stay up
// before we treat it as a successful subscription (used to reset the
// failover backoff). Below this, we count it as "dialed and immediately
// dropped" — same shape as a rejection.
const fullnodeHealthyConnDuration = 30 * time.Second

// fullnodeMaxBackoff caps the inter-cycle backoff. Persistent failure
// (all validators down, or all at their inbound fullnode cap) is an
// operator-fixable condition that doesn't justify aborting the process,
// so we stop doubling once the wait reaches this value and just keep
// retrying at that rate.
const fullnodeMaxBackoff = 5 * time.Minute

// runFullnodeSubscriber implements the fullnode single-active-subscriber
// dial loop: pick a committee member, dial + run block sync against it,
// advance to the next on disconnect or rejection. Loops forever; exits
// only when ctx is cancelled.
//
// The committee list is shuffled once at startup so multiple fullnode
// nodes don't all converge on the same first-choice validator (which
// would imbalance load across the committee). Each node's starting
// preference is independently random; failover preserves that order.
//
// Backoff: between attempts within a cycle we sleep cfg.DialInterval
// (matches the validator side). After a full pass with no healthy
// connection we double the inter-attempt sleep, capped at
// fullnodeMaxBackoff so a persistently-saturated cluster (e.g. all
// validators at their inbound fullnode cap) keeps retrying at a polite
// rate instead of unbounded. A single attempt that runs longer than
// fullnodeHealthyConnDuration is treated as a successful subscription and
// resets the backoff to cfg.DialInterval.
//
// TODO(autobahn-state-sync): block sync from a single peer is bounded by
// that peer's per-stream rate limit (GetBlock's rpc.Limit{Rate:10,
// Concurrent:10}), which makes initial catch-up of a node joining an
// established cluster slow — minutes per few thousand blocks. The
// long-term fix is autobahn snapshot transfer (CometBFT-style state
// sync), letting a fresh fullnode jump to recent state instead of
// replaying from genesis. This loop is correct for "fresh node on a
// fresh cluster" and "restart of a near-tip node" — the two cases
// production fullnodes hit once state sync lands.
func (r *gigaFullnodeRouter) runFullnodeSubscriber(ctx context.Context) error {
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
		if time.Since(start) >= fullnodeHealthyConnDuration {
			// Connection ran long enough to count as accepted; reset.
			backoff = r.cfg.DialInterval
			failsThisCycle = 0
			lastHealthyOrStart = time.Now()
		} else {
			failsThisCycle++
		}
		logger.Info("fullnode giga connection ended; failing over",
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
			if time.Since(lastHealthyOrStart) >= fullnodeHealthyConnDuration {
				backoff = min(backoff*2, fullnodeMaxBackoff)
			}
			failsThisCycle = 0
		}
	}
}

package p2p

import (
	"context"
	"maps"
	"math/rand/v2"
	"net/url"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type gigaFullnodeRouter struct {
	*gigaRouterCommon
}

func (r *gigaFullnodeRouter) MaxGasEstimatedPerBlock() uint64 {
	if r.cfg.GenDoc.ConsensusParams != nil {
		return r.cfg.GenDoc.ConsensusParams.Block.MaxGasUint64()
	}
	return 0
}

func (r *gigaFullnodeRouter) AsValidator() utils.Option[GigaValidatorRouter] {
	return utils.None[GigaValidatorRouter]()
}

// EvmProxy on a fullnode always forwards — no validator key, no local
// mempool, no self-shard short-circuit. validateCommonAndBuildData
// rejects configs missing any URL, so .Get() never silent-drops in production.
func (r *gigaFullnodeRouter) EvmProxy(sender common.Address) (*url.URL, bool) {
	shardValidator := r.data.Committee().EvmShard(sender)
	return r.cfg.ValidatorAddrs[shardValidator].EVMRPC.Get()
}

func (r *gigaFullnodeRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		if err := r.seedLastExecuted(ctx); err != nil {
			return err
		}
		// Single-active subscriber: walk the committee in a stable order,
		// move to the next on disconnect. Avoids the N× QC duplication of
		// fanning out to every committee member.
		//
		// TODO(autobahn-fullnode): allow hard-configuring a preferred
		// validator (or a subset of trusted validators) instead of walking
		// the whole committee.
		s.Spawn(func() error { return r.runFullnodeSubscriber(ctx) })
		s.SpawnNamed("data", func() error { return r.data.Run(ctx) })
		s.SpawnNamed("execute", func() error { return r.runExecute(ctx) })
		s.SpawnNamed("service", func() error { return r.service.Run(ctx) })
		return nil
	})
}

// fullnodeHealthyConnDuration: a connection that lasts at least this long
// counts as a successful subscription (resets backoff). Below this, treat
// the same as a rejection.
const fullnodeHealthyConnDuration = 30 * time.Second

// fullnodeMaxBackoff caps the inter-cycle backoff so a persistently-
// saturated cluster keeps retrying at a polite rate instead of unbounded.
const fullnodeMaxBackoff = 5 * time.Minute

// runFullnodeSubscriber: pick a committee member, dial + block-sync,
// advance on disconnect/reject. Committee list shuffled once at startup so
// multiple fullnodes don't all converge on the same first choice.
//
// Backoff: cfg.DialInterval between attempts within a cycle. After a full
// pass with no healthy connection, double (capped at fullnodeMaxBackoff)
// — but only if time-since-last-healthy exceeds fullnodeHealthyConnDuration,
// so a brief post-reconnect burst of failures doesn't escalate.
//
// TODO(autobahn-state-sync): block sync from a single peer is bounded by
// GetBlock's per-stream rate limit (rpc.Limit{Rate:10, Concurrent:10}) —
// initial catch-up of a fresh node joining an established cluster is
// slow. Long-term fix is autobahn snapshot transfer (CometBFT-style state
// sync). This loop is correct for "fresh cluster" and "restart of a
// near-tip node."
func (r *gigaFullnodeRouter) runFullnodeSubscriber(ctx context.Context) error {
	addrs := slices.Collect(maps.Values(r.cfg.ValidatorAddrs))
	rand.Shuffle(len(addrs), func(i, j int) { addrs[i], addrs[j] = addrs[j], addrs[i] })
	backoff := r.cfg.DialInterval
	failsThisCycle := 0
	// Most recent healthy connection (or loop start). Gates backoff
	// escalation against transient post-reconnect failure bursts.
	lastHealthyOrStart := time.Now()
	for i := 0; ; i = (i + 1) % len(addrs) {
		addr := addrs[i]
		start := time.Now()
		err := r.dialAndRunConn(ctx, addr.Key, addr.HostPort, r.service.RunBlockSyncClient)
		if time.Since(start) >= fullnodeHealthyConnDuration {
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
		if failsThisCycle >= len(addrs) {
			if time.Since(lastHealthyOrStart) >= fullnodeHealthyConnDuration {
				backoff = min(backoff*2, fullnodeMaxBackoff)
			}
			failsThisCycle = 0
		}
	}
}

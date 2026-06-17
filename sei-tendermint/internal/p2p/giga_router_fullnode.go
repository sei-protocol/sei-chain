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
// AND delivered new blocks (data.NextBlock advanced) counts as a successful
// subscription and resets backoff. Below this, treat the same as a rejection.
const fullnodeHealthyConnDuration = 30 * time.Second

// fullnodeMaxBackoff caps the inter-cycle backoff so a persistently-
// saturated cluster keeps retrying at a polite rate instead of unbounded.
const fullnodeMaxBackoff = 5 * time.Minute

// fullnodeStallTimeout: close the connection if data.NextBlock has not
// advanced for this long. Bounds the time a single peer can hold the loop
// open without delivering blocks.
const fullnodeStallTimeout = 60 * time.Second

// fullnodeProgressPollInterval: how often watchProgress samples
// data.NextBlock.
const fullnodeProgressPollInterval = 5 * time.Second

// runFullnodeSubscriber: pick a committee member, dial + block-sync,
// advance on disconnect/reject/stall. Committee list shuffled once at
// startup so multiple fullnodes don't all converge on the same first
// choice.
//
// Stall: watchProgress runs alongside the connection and closes it if
// data.NextBlock stays unchanged for fullnodeStallTimeout. A stalled
// disconnect counts as a failure (no progress) and lets backoff climb,
// so a cluster-wide stall self-throttles instead of churning peers.
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
		startHeight := r.data.NextBlock()
		err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			s.SpawnBg(func() error { return r.watchProgress(ctx) })
			return r.dialAndRunConn(ctx, addr.Key, addr.HostPort, r.service.RunBlockSyncClient)
		})
		// Healthy reset is gated on actual progress (data.NextBlock
		// advanced), not connection duration alone. A peer that holds
		// the connection open without delivering blocks counts as a
		// failure, so backoff keeps climbing through such disconnects.
		madeProgress := r.data.NextBlock() > startHeight
		if madeProgress && time.Since(start) >= fullnodeHealthyConnDuration {
			backoff = r.cfg.DialInterval
			failsThisCycle = 0
			lastHealthyOrStart = time.Now()
		} else {
			failsThisCycle++
		}
		logger.Info("fullnode giga connection ended; failing over",
			"addr", addr, "err", err, "progress", madeProgress, "backoff", backoff)
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

// watchProgress returns an error once data.NextBlock has stayed unchanged
// for fullnodeStallTimeout. Runs alongside the dial in scope.Run; its error
// cancels the connection and lets the loop fail over.
func (r *gigaFullnodeRouter) watchProgress(ctx context.Context) error {
	last := r.data.NextBlock()
	lastChange := time.Now()
	for {
		if err := utils.Sleep(ctx, fullnodeProgressPollInterval); err != nil {
			return err
		}
		if cur := r.data.NextBlock(); cur > last {
			last = cur
			lastChange = time.Now()
			continue
		}
		if time.Since(lastChange) >= fullnodeStallTimeout {
			return fmt.Errorf("no block-sync progress for %v", fullnodeStallTimeout)
		}
	}
}

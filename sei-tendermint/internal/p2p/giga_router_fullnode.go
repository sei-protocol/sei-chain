package p2p

import (
	"context"
	"maps"
	"math/rand/v2"
	"slices"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type gigaFullnodeRouter struct {
	*gigaRouterCommon
}

// NewGigaFullnodeRouter constructs a fullnode GigaRouter over an already-built
// data.State. The caller owns the BlockDB that backs dataState (see BuildDataState).
func NewGigaFullnodeRouter(cfg *GigaRouterCommonConfig, key NodeSecretKey, dataState *data.State) (*gigaFullnodeRouter, error) {
	logger.Info("GigaRouter initialized (fullnode)", "validators", len(cfg.ValidatorAddrs), "dial_interval", cfg.DialInterval, "inbound_fullnode_cap", cfg.MaxInboundFullnodePeers)
	return &gigaFullnodeRouter{
		gigaRouterCommon: &gigaRouterCommon{
			cfg:                cfg,
			key:                key,
			data:               dataState,
			service:            giga.NewBlockSyncService(dataState),
			poolIn:             giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
			poolOut:            giga.NewPool[NodePublicKey, rpc.Client[giga.API]](),
			app:                cfg.App,
			inboundFullnodeCap: int64(cfg.MaxInboundFullnodePeers),
		},
	}, nil
}

func (r *gigaFullnodeRouter) Mempool() utils.Option[*producer.State] {
	return utils.None[*producer.State]()
}

func (r *gigaFullnodeRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
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

// runFullnodeSubscriber: pick a committee member, dial + block-sync,
// advance on disconnect/reject. Committee list shuffled once at startup
// so multiple fullnodes don't all converge on the same first choice.
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
	for i := 0; ; i = (i + 1) % len(addrs) {
		addr := addrs[i]
		err := r.dialAndRunConn(ctx, utils.None[NodePublicKey](), addr.HostPort, r.service.RunBlockSyncClient)
		logger.Info("fullnode giga connection ended; failing over", "addr", addr, "err", err)
		if err := utils.Sleep(ctx, r.cfg.DialInterval); err != nil {
			return err
		}
	}
}

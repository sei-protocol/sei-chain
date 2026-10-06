package p2p

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
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
			nextCommitEpoch:    dataState.NextCommitEpoch(),
			anchor:             dataState.Anchor(),
			service:            giga.NewFullNodeService(dataState),
			poolIn:             giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
			poolInCommittee:    giga.NewPool[atypes.PublicKey, rpc.Server[giga.API]](),
			poolOut:            giga.NewPool[atypes.PublicKey, rpc.Client[giga.API]](),
			proxies:            utils.NewRWMutex(map[atypes.PublicKey]*ethrpc.Client{}),
			app:                cfg.App,
			liveAddrs:          utils.NewRWMutex(map[atypes.PublicKey]GigaNodeAddr{}),
			liveAddrVersion:    utils.NewAtomicSend(uint64(0)),
			executed:           utils.NewAtomicSend(atypes.NewExecutedBlocks(atypes.ExecutedBlock{Number: utils.Clamp[atypes.GlobalBlockNumber](cfg.App.LastBlockHeight())})),
			inboundFullnodeCap: int64(cfg.MaxInboundFullnodePeers),
		},
	}, nil
}

func (r *gigaFullnodeRouter) Mempool() utils.Option[*producer.State] {
	return utils.None[*producer.State]()
}

// Run block-syncs from every committee member at once and executes the
// finalized blocks. The fullnode service has no consensus state, so each
// connection carries only the QC streams, ping, and GetBlock.
//
// TODO(autobahn-fullnode): allow configuring a subset of committee members to
// block-sync from, so each validator's inbound fullnode cap does not bound the
// fullnode fleet. The EVM proxy still needs every shard owner.
func (r *gigaFullnodeRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnNamed("committeeMembers", func() error {
			return r.runPerCommitteeMember(ctx, r.runCommitteePeer, r.runEvmProxy)
		})
		s.SpawnNamed("data", func() error { return r.data.Run(ctx) })
		s.SpawnNamed("execute", func() error { return r.runExecute(ctx) })
		s.SpawnNamed("service", func() error { return r.service.Run(ctx) })
		return nil
	})
}

// EvmProxyEnabled is always true: fullnodes have no local mempool and proxy
// every transaction.
func (r *gigaFullnodeRouter) EvmProxyEnabled() bool { return true }

// EvmProxy on the fullnode always returns the shard owner's EVM RPC client.
// EnableEvmProxy is a no-op here because fullnodes do not have a local mempool.
func (r *gigaFullnodeRouter) EvmProxy(sender common.Address) utils.Option[*ethrpc.Client] {
	return r.evmProxy(r.nextCommitEpoch.Load().Committee().EvmShard(sender))
}

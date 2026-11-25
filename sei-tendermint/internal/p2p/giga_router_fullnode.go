package p2p

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type gigaFullnodeRouter struct {
	*gigaRouterCommon
}

// NewGigaFullnodeRouter constructs a fullnode GigaRouter over an already-built
// data.State. The caller owns the BlockDB that backs dataState (see BuildDataState).
func NewGigaFullnodeRouter(cfg *GigaRouterCommonConfig, key NodeSecretKey, dataState *data.State, signer *abevm.Signer) (*gigaFullnodeRouter, error) {
	logger.Info("GigaRouter initialized (fullnode)", "validators", len(cfg.ValidatorAddrs), "dial_interval", cfg.DialInterval, "inbound_fullnode_cap", cfg.MaxInboundFullnodePeers, "persistent_peers", len(cfg.PersistentPeerIDs))
	return &gigaFullnodeRouter{
		gigaRouterCommon: &gigaRouterCommon{
			cfg:                  cfg,
			key:                  key,
			data:                 dataState,
			service:              giga.NewFullNodeService(dataState),
			poolIn:               giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
			poolInCommittee:      giga.NewPool[atypes.PublicKey, rpc.Server[giga.API]](),
			poolOut:              giga.NewPool[atypes.PublicKey, rpc.Client[giga.API]](),
			proxies:              utils.NewRWMutex(map[atypes.PublicKey]*ethrpc.Client{}),
			signer:               signer,
			inboundFullnodeCount: utils.NewMutex(new(int64)),
			inboundFullnodeCap:   int64(cfg.MaxInboundFullnodePeers),
		},
	}, nil
}

func (r *gigaFullnodeRouter) Mempool() utils.Option[*producer.State] {
	return utils.None[*producer.State]()
}

func (r *gigaFullnodeRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return r.runCommitteeConnections(ctx) })
		s.SpawnNamed("data", func() error { return r.data.Run(ctx) })
		s.SpawnNamed("pushAppHashes", func() error { return r.runPushAppHashes(ctx) })
		s.SpawnNamed("service", func() error { return r.service.Run(ctx) })
		return nil
	})
}

// EvmProxy on the fullnode always returns the shard owner's EVM RPC client.
// EnableEvmProxy is a no-op here because fullnodes do not have a local mempool.
func (r *gigaFullnodeRouter) EvmProxy(sender common.Address) utils.Option[*ethrpc.Client] {
	return r.evmProxy(r.data.Anchor().Load().NextHeightEpoch().Committee().EvmShard(sender))
}

// runCommitteeConnections maintains block-sync and EVM RPC connections to every committee member.
func (r *gigaFullnodeRouter) runCommitteeConnections(ctx context.Context) error {
	return r.runPerCommitteeMember(ctx, true, func(ctx context.Context, validator atypes.PublicKey, addr GigaNodeAddr) error {
		for {
			err := r.dialAndRunConn(ctx, validator, addr.Key, addr.HostPort, func(ctx context.Context, client rpc.Client[giga.API]) error {
				return r.service.RunClient(ctx, client, validator, true)
			})
			logger.Info("fullnode giga connection ended", "addr", addr, "err", err)
			if err := utils.Sleep(ctx, r.cfg.DialInterval); err != nil {
				return err
			}
		}
	})
}

// InsertTx submits input to its sender's shard and returns the transaction hash.
func (r *gigaFullnodeRouter) InsertTx(ctx context.Context, input types.Tx) (common.Hash, error) {
	return r.gigaRouterCommon.insertTx(ctx, input, r)
}

// EvmPendingNonce returns the next nonce admitted by the sender's shard.
func (r *gigaFullnodeRouter) EvmPendingNonce(ctx context.Context, address common.Address) (uint64, error) {
	return r.gigaRouterCommon.evmPendingNonce(ctx, address, r)
}

package p2p

import (
	"context"

	ethrpc "github.com/ethereum/go-ethereum/rpc"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type gigaFullnodeRouter struct {
	*GigaRouterCommon
}

// NewGigaFullnodeRouter constructs a fullnode GigaRouter over an already-built
// data.State. The caller owns the BlockDB that backs dataState (see BuildDataState).
func NewGigaFullnodeRouter(cfg *GigaRouterCommonConfig, key NodeSecretKey, dataState *data.State, signer *abevm.Signer) (*gigaFullnodeRouter, error) {
	validatorAddrs := newValidatorAddrs(dataState)
	logger.Info("GigaRouter initialized (fullnode)", "validators", len(cfg.ValidatorAddrs), "dial_interval", cfg.DialInterval, "inbound_fullnode_cap", cfg.MaxInboundFullnodePeers, "persistent_peers", len(cfg.PersistentPeerIDs))
	return &gigaFullnodeRouter{
		GigaRouterCommon: &GigaRouterCommon{
			cfg:                  cfg,
			key:                  key,
			data:                 dataState,
			service:              giga.NewFullNodeService(dataState, validatorAddrs),
			poolIn:               giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
			poolInCommittee:      giga.NewPool[atypes.PublicKey, rpc.Server[giga.API]](),
			poolOut:              giga.NewPool[atypes.PublicKey, rpc.Client[giga.API]](),
			proxies:              utils.NewRWMutex(map[atypes.PublicKey]*ethrpc.Client{}),
			signer:               signer,
			validatorAddrs:       validatorAddrs,
			inboundFullnodeCount: utils.NewMutex(new(int64)),
			inboundFullnodeCap:   int64(cfg.MaxInboundFullnodePeers),
		},
	}, nil
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

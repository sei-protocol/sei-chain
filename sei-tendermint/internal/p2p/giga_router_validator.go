package p2p

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ethereum/go-ethereum/common"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type gigaValidatorRouter struct {
	*gigaRouterCommon

	blockDB   atypes.BlockDB
	consensus *consensus.State
	producer  *producer.State
	// validatorKey is the cached public form of cfg.ValidatorKey, used by
	// EvmProxy to short-circuit self-shard sends to the local mempool.
	validatorKey atypes.PublicKey
}

func NewGigaValidatorRouter(cfg *GigaValidatorConfig, key NodeSecretKey) (*gigaValidatorRouter, error) {
	dataState, blockDB, err := buildDataState(&cfg.GigaRouterCommonConfig)
	if err != nil {
		return nil, err
	}
	consensusState, err := consensus.NewState(&consensus.Config{
		Key:                cfg.ValidatorKey,
		ViewTimeout:        cfg.ViewTimeout,
		PersistentStateDir: cfg.PersistentStateDir,
	}, dataState)
	if err != nil {
		_ = blockDB.Close()
		return nil, fmt.Errorf("consensus.NewState(): %w", err)
	}
	producerState := producer.NewState(cfg.Producer, consensusState, cfg.App)
	logger.Info("GigaRouter initialized (validator)", "validators", len(cfg.ValidatorAddrs), "dial_interval", cfg.DialInterval, "inbound_fullnode_cap", cfg.MaxInboundFullnodePeers)
	return &gigaValidatorRouter{
		gigaRouterCommon: &gigaRouterCommon{
			cfg:                &cfg.GigaRouterCommonConfig,
			key:                key,
			data:               dataState,
			service:            giga.NewService(consensusState),
			poolIn:             giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
			poolOut:            giga.NewPool[NodePublicKey, rpc.Client[giga.API]](),
			app:                cfg.App,
			inboundFullnodeCap: int64(cfg.MaxInboundFullnodePeers),
		},
		blockDB:      blockDB,
		consensus:    consensusState,
		producer:     producerState,
		validatorKey: cfg.ValidatorKey.Public(),
	}, nil
}

func (r *gigaValidatorRouter) Mempool() utils.Option[*producer.State] {
	return utils.Some(r.producer)
}

func (r *gigaValidatorRouter) Run(ctx context.Context) error {
	defer func() { _ = r.blockDB.Close() }()
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// Validators dial every committee member in parallel — consensus
		// voting needs fan-out, not stickiness. Same connections also
		// serve block sync between committee peers.
		for _, addr := range r.cfg.ValidatorAddrs {
			s.Spawn(func() error {
				for {
					err := r.dialAndRunConn(ctx, utils.Some(addr.Key), addr.HostPort, r.service.RunClient)
					logger.Info("giga connection failed", "addr", addr, "err", err)
					if err := utils.Sleep(ctx, r.cfg.DialInterval); err != nil {
						return err
					}
				}
			})
		}
		s.SpawnNamed("consensus", func() error { return r.consensus.Run(ctx) })
		s.SpawnNamed("producer", func() error { return r.producer.Run(ctx) })
		s.SpawnNamed("data", func() error { return r.data.Run(ctx) })
		s.SpawnNamed("execute", func() error { return r.runExecute(ctx) })
		s.SpawnNamed("service", func() error { return r.service.Run(ctx) })
		return nil
	})
}

// EvmProxy on the validator returns None when the sender's shard owner is
// us (handle locally via mempool, no HTTP round-trip to self).
func (r *gigaValidatorRouter) EvmProxy(sender common.Address) utils.Option[*url.URL] {
	shardValidator := r.data.Registry().LatestEpoch().Committee().EvmShard(sender)
	if r.validatorKey == shardValidator {
		return utils.None[*url.URL]()
	}
	return utils.Some(r.cfg.ValidatorAddrs[shardValidator].EVMRPC)
}

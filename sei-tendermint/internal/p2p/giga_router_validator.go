package p2p

import (
	"context"
	"fmt"

	"golang.org/x/time/rate"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type gigaValidatorRouter struct {
	*gigaRouterCommon

	consensus *consensus.State
	producer  *producer.State
}

// NewGigaValidatorRouter constructs a validator GigaRouter over an already-built
// data.State. The caller owns the BlockDB that backs dataState (see BuildDataState);
// close it if this constructor returns an error.
func NewGigaValidatorRouter(cfg *GigaValidatorConfig, key NodeSecretKey, dataState *data.State, signer *abevm.Signer) (*gigaValidatorRouter, error) {
	validatorKey := cfg.ValidatorKey.Public()
	self, ok := cfg.ValidatorAddrs[validatorKey]
	if !ok {
		return nil, fmt.Errorf("local validator %v has no configured giga address", validatorKey)
	}
	if self.Key != key.Public() {
		return nil, fmt.Errorf("local validator node key = %v, want %v", self.Key, key.Public())
	}
	if err := utils.CheckRPCURL(cfg.EVMRPC); err != nil {
		return nil, fmt.Errorf("local validator %v evmrpc: %w", validatorKey, err)
	}
	selfAddr := NodeAddress{
		NodeID:   key.Public().NodeID(),
		Hostname: self.HostPort.Hostname,
		Port:     self.HostPort.Port,
	}
	// An invalid local address makes every peer reject our giga claim.
	if err := selfAddr.Validate(); err != nil {
		return nil, fmt.Errorf("local validator %v address: %w", validatorKey, err)
	}
	consensusState, err := consensus.NewState(&consensus.Config{
		Key:                cfg.ValidatorKey,
		ViewTimeout:        cfg.ViewTimeout,
		ProposalTimeout:    cfg.ProposalTimeout,
		MaxRate:            rate.Every(cfg.GenDoc.Autobahn.Or(types.DefaultAutobahnParams()).MinRoadBlockInterval),
		PersistentStateDir: utils.Some(cfg.PersistentStateDir),
	}, dataState)
	if err != nil {
		return nil, fmt.Errorf("consensus.NewState(): %w", err)
	}
	producerState := producer.NewState(cfg.Producer, consensusState, cfg.App, signer)
	logger.Info("GigaRouter initialized (validator)", "validators", len(cfg.ValidatorAddrs), "dial_interval", cfg.DialInterval, "inbound_fullnode_cap", cfg.MaxInboundFullnodePeers, "persistent_peers", len(cfg.PersistentPeerIDs))
	return &gigaValidatorRouter{
		gigaRouterCommon: &gigaRouterCommon{
			cfg:             &cfg.GigaRouterCommonConfig,
			key:             key,
			data:            dataState,
			service:         giga.NewService(consensusState),
			poolIn:          giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
			poolInCommittee: giga.NewPool[atypes.PublicKey, rpc.Server[giga.API]](),
			poolOut:         giga.NewPool[atypes.PublicKey, rpc.Client[giga.API]](),
			proxies:         utils.NewRWMutex(map[atypes.PublicKey]*ethrpc.Client{}),
			app:             cfg.App,
			signer:          signer,
			gigaSpec: gigaHandshakeSpec{
				ValidatorKey: utils.Some(cfg.ValidatorKey),
				EVMRPC:       cfg.EVMRPC,
			},
			selfAddr: utils.Some(selfAddr),

			inboundFullnodeCount: utils.NewMutex(new(int64)),
			inboundFullnodeCap:   int64(cfg.MaxInboundFullnodePeers),
		},
		consensus: consensusState,
		producer:  producerState,
	}, nil
}

func (r *gigaValidatorRouter) Mempool() utils.Option[*producer.State] {
	return utils.Some(r.producer)
}

func (r *gigaValidatorRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnNamed("committeeMembers", func() error {
			return r.runPerCommitteeMember(ctx, false, r.runCommitteePeer)
		})
		s.SpawnNamed("consensus", func() error { return r.consensus.Run(ctx) })
		s.SpawnNamed("producer", func() error { return r.producer.Run(ctx) })
		s.SpawnNamed("data", func() error { return r.data.Run(ctx) })
		s.SpawnNamed("pushAppHashes", func() error { return r.runPushAppHashes(ctx) })
		s.SpawnNamed("service", func() error { return r.service.Run(ctx) })
		return nil
	})
}

// runCommitteePeer maintains an outbound giga connection to a committee member.
// Self disables GetBlock: a loopback consumer always returns empty for missing
// catch-up heights and can starve the contiguous prefix while higher gap-fills
// keep retrying. Compare against the p2p node key (r.key.Public), not
// validatorKey (consensus signing key): GigaNodeAddr.Key is a
// NodePublicKey.
func (r *gigaValidatorRouter) runCommitteePeer(ctx context.Context, validatorKey atypes.PublicKey, addr GigaNodeAddr) error {
	getBlock := addr.Key != r.key.Public()
	for {
		err := r.dialAndRunConn(ctx, validatorKey, addr.Key, addr.HostPort, func(ctx context.Context, client rpc.Client[giga.API]) error {
			return r.service.RunClient(ctx, client, validatorKey, getBlock)
		})
		logger.Info("giga connection failed", "addr", addr, "err", err)
		if err := utils.Sleep(ctx, r.cfg.DialInterval); err != nil {
			return err
		}
	}
}

// EvmProxyEnabled is always false, because a validator admits every EVM tx it
// receives into its own mempool.
func (r *gigaValidatorRouter) EvmProxyEnabled() bool { return false }

// EvmProxy on the validator always returns None. Validators never forward EVM
// txs to another validator, and full nodes choose the lane.
func (r *gigaValidatorRouter) EvmProxy(common.Address) utils.Option[*ethrpc.Client] {
	return utils.None[*ethrpc.Client]()
}

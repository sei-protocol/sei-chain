package p2p

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type gigaValidatorRouter struct {
	*gigaRouterCommon

	consensus *consensus.State
	producer  *producer.State
	// validatorKey is the cached public form of cfg.ValidatorKey, used by
	// EvmProxy to short-circuit self-shard sends to the local mempool.
	validatorKey atypes.PublicKey
}

// NewGigaValidatorRouter constructs a validator GigaRouter over an already-built
// data.State. The caller owns the BlockDB that backs dataState (see BuildDataState);
// close it if this constructor returns an error.
func NewGigaValidatorRouter(cfg *GigaValidatorConfig, key NodeSecretKey, dataState *data.State) (*gigaValidatorRouter, error) {
	validatorKey := cfg.ValidatorKey.Public()
	self, ok := cfg.ValidatorAddrs[validatorKey]
	if !ok {
		return nil, fmt.Errorf("local validator %v has no configured giga address", validatorKey)
	}
	if self.Key != key.Public() {
		return nil, fmt.Errorf("local validator node key = %v, want %v", self.Key, key.Public())
	}
	if err := utils.CheckHTTPURL(self.EVMRPC); err != nil {
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
		PersistentStateDir: cfg.PersistentStateDir,
	}, dataState)
	if err != nil {
		return nil, fmt.Errorf("consensus.NewState(): %w", err)
	}
	producerState := producer.NewState(cfg.Producer, consensusState, cfg.App)
	logger.Info("GigaRouter initialized (validator)", "validators", len(cfg.ValidatorAddrs), "dial_interval", cfg.DialInterval, "inbound_fullnode_cap", cfg.MaxInboundFullnodePeers)
	return &gigaValidatorRouter{
		gigaRouterCommon: &gigaRouterCommon{
			cfg:             &cfg.GigaRouterCommonConfig,
			key:             key,
			data:            dataState,
			nextCommitEpoch: dataState.NextCommitEpoch(),
			anchor:          dataState.Anchor(),
			service:         giga.NewService(consensusState),
			poolIn:          giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
			poolInCommittee: giga.NewPool[atypes.PublicKey, rpc.Server[giga.API]](),
			poolOut:         giga.NewPool[atypes.PublicKey, rpc.Client[giga.API]](),
			proxies:         utils.NewRWMutex(map[atypes.PublicKey]*ethrpc.Client{}),
			app:             cfg.App,
			offer: utils.Some(handshakeOffer{
				ValidatorKey: cfg.ValidatorKey,
				EVMRPC:       self.EVMRPC,
			}),
			selfAddr:           utils.Some(selfAddr),
			liveAddrs:          utils.NewRWMutex(map[atypes.PublicKey]GigaNodeAddr{}),
			liveAddrVersion:    utils.NewAtomicSend(uint64(0)),
			inboundFullnodeCap: int64(cfg.MaxInboundFullnodePeers),
		},
		consensus:    consensusState,
		producer:     producerState,
		validatorKey: validatorKey,
	}, nil
}

func (r *gigaValidatorRouter) Mempool() utils.Option[*producer.State] {
	return utils.Some(r.producer)
}

func (r *gigaValidatorRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnNamed("committeeMembers", func() error {
			return r.runPerCommitteeMember(ctx, r.runCommitteePeer, r.runEvmProxy)
		})
		s.SpawnNamed("consensus", func() error { return r.consensus.Run(ctx) })
		s.SpawnNamed("producer", func() error { return r.producer.Run(ctx) })
		s.SpawnNamed("data", func() error { return r.data.Run(ctx) })
		s.SpawnNamed("execute", func() error { return r.runExecute(ctx) })
		s.SpawnNamed("service", func() error { return r.service.Run(ctx) })
		return nil
	})
}

// runCommitteePeer maintains an outbound giga connection to a committee member.
// Self disables GetBlock: a loopback consumer always returns empty for missing
// catch-up heights and can starve the contiguous prefix while higher gap-fills
// keep retrying. Compare against the p2p node key (r.key.Public), not
// validatorKey (consensus signing key used by EvmProxy): GigaNodeAddr.Key is a
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

// EvmProxy on the validator returns None when the sender's shard owner is
// us (handle locally via mempool). For remote
// shards, we proxy only while the target validator is currently connected;
// otherwise we keep the tx local as a best-effort availability heuristic.
func (r *gigaValidatorRouter) EvmProxy(sender common.Address) utils.Option[*ethrpc.Client] {
	if !r.cfg.EnableEvmProxy {
		return utils.None[*ethrpc.Client]()
	}
	validator := r.nextCommitEpoch.Load().Committee().EvmShard(sender)
	if r.validatorKey == validator {
		return utils.None[*ethrpc.Client]()
	}
	if _, ok := r.poolOut.Get(validator); !ok {
		return utils.None[*ethrpc.Client]()
	}
	return r.evmProxy(validator)
}

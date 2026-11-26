package p2p

import (
	"context"
	"hash/fnv"

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
			app:                  cfg.App,
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

// EvmProxyEnabled is always true: fullnodes have no local mempool and proxy
// every transaction.
func (r *gigaFullnodeRouter) EvmProxyEnabled() bool { return true }

// EvmProxy on the fullnode returns the shard owner's EVM RPC client or, when the
// owner is not connected, a relay validator's client. It returns None only when no
// validator is connected. EnableEvmProxy is a no-op here because fullnodes do not
// have a local mempool.
func (r *gigaFullnodeRouter) EvmProxy(sender common.Address) utils.Option[*ethrpc.Client] {
	if client, ok := r.evmProxy(r.data.Anchor().Load().NextHeightEpoch().Committee().EvmShard(sender)).Get(); ok {
		return utils.Some(client)
	}
	return r.relayEvmProxy(sender)
}

// relayEvmProxy returns the EVM RPC client of a connected validator chosen by
// sender, or None if no validator is connected.
func (r *gigaFullnodeRouter) relayEvmProxy(sender common.Address) utils.Option[*ethrpc.Client] {
	for proxies := range r.proxies.RLock() {
		// The relay forwards to the shard owner when connected to it, and otherwise
		// keeps the tx in its own mempool. Rendezvous hashing keeps a sender's sends
		// and pending-nonce queries on the same relay unless that relay disconnects,
		// and spreads senders across relays.
		var best utils.Option[atypes.PublicKey]
		var bestScore uint64
		for validator := range proxies {
			score := relayScore(sender, validator)
			if b, ok := best.Get(); !ok || score > bestScore || (score == bestScore && validator.Compare(b) < 0) {
				best, bestScore = utils.Some(validator), score
			}
		}
		return best.Map(func(v atypes.PublicKey) *ethrpc.Client { return proxies[v] })
	}
	panic("unreachable")
}

// relayScore returns the rendezvous-hashing weight of validator as a relay for sender.
func relayScore(sender common.Address, validator atypes.PublicKey) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(sender[:])
	_, _ = h.Write(validator.Bytes())
	return h.Sum64()
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

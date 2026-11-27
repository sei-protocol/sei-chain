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
)

type gigaFullnodeRouter struct {
	*gigaRouterCommon

	// lanes picks the lane that EvmProxy forwards each sender's txs to.
	lanes *laneAssignments
	// proxies holds an EVM RPC client for each validator with a live connection.
	proxies utils.RWMutex[map[atypes.PublicKey]*ethrpc.Client]
}

// NewGigaFullnodeRouter constructs a fullnode GigaRouter over an already-built
// data.State. The caller owns the BlockDB that backs dataState (see BuildDataState).
func NewGigaFullnodeRouter(cfg *GigaRouterCommonConfig, key NodeSecretKey, dataState *data.State, signer *abevm.Signer) (*gigaFullnodeRouter, error) {
	logger.Info("GigaRouter initialized (fullnode)", "validators", len(cfg.ValidatorAddrs), "dial_interval", cfg.DialInterval, "inbound_fullnode_cap", cfg.MaxInboundFullnodePeers, "persistent_peers", len(cfg.PersistentPeerIDs))
	// A new seed at each start gives each full node, and each restart, its own
	// sender-to-lane mapping. The seed is never stored or exposed.
	seed := newLaneSeed()
	return &gigaFullnodeRouter{
		lanes:   newLaneAssignments(func(c *atypes.Committee) laneAssigner { return newLaneRing(seed, c) }),
		proxies: utils.NewRWMutex(map[atypes.PublicKey]*ethrpc.Client{}),
		gigaRouterCommon: &gigaRouterCommon{
			cfg:                  cfg,
			key:                  key,
			data:                 dataState,
			service:              giga.NewFullNodeService(dataState),
			poolIn:               giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
			poolInCommittee:      giga.NewPool[atypes.PublicKey, rpc.Server[giga.API]](),
			poolOut:              giga.NewPool[atypes.PublicKey, rpc.Client[giga.API]](),
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

// EvmProxy returns the EVM RPC client of the lane that this fullnode assigns to
// sender, skipping lanes it has no EVM RPC client for. It returns None when it
// has a client for no lane of the committee.
func (r *gigaFullnodeRouter) EvmProxy(sender common.Address) utils.Option[*ethrpc.Client] {
	assigner := r.lanes.For(r.data.Anchor().Load().NextHeightEpoch().Committee())
	for proxies := range r.proxies.RLock() {
		reachable := func(v atypes.PublicKey) bool { _, ok := proxies[v]; return ok }
		if v, ok := assigner.Lane(sender, reachable).Get(); ok {
			return utils.Some(proxies[v])
		}
	}
	return utils.None[*ethrpc.Client]()
}

// runCommitteeConnections maintains block-sync and EVM RPC connections to every committee member.
func (r *gigaFullnodeRouter) runCommitteeConnections(ctx context.Context) error {
	return r.runPerCommitteeMember(ctx, true, func(ctx context.Context, validator atypes.PublicKey, addr GigaNodeAddr) error {
		for {
			err := r.dialAndRunConn(ctx, validator, addr.Key, addr.HostPort, func(ctx context.Context, client rpc.Client[giga.API], peer validatorHandshake) error {
				return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
					s.Spawn(func() error { return r.runEvmProxy(ctx, peer) })
					return r.service.RunClient(ctx, client, validator, true)
				})
			})
			logger.Info("fullnode giga connection ended", "addr", addr, "err", err)
			if err := utils.Sleep(ctx, r.cfg.DialInterval); err != nil {
				return err
			}
		}
	})
}

// runEvmProxy maintains an EVM RPC client for the lifetime of its Giga connection.
func (r *gigaFullnodeRouter) runEvmProxy(ctx context.Context, validator validatorHandshake) error {
	if !r.cfg.AllowLocalAddrs && utils.IsLoopbackOrLinkLocalURL(validator.EVMRPC) {
		logger.Error("committee member advertised an unroutable EVM RPC; not learning its address",
			"validator", validator.auth.Key(), "evmRPC", validator.EVMRPC.String())
		return nil
	}
	var client *ethrpc.Client
	for {
		var err error
		client, err = ethrpc.DialContext(ctx, validator.EVMRPC.String())
		if err == nil {
			defer client.Close()
			break
		}
		logger.Info("evm proxy dial failed", "url", validator.EVMRPC, "err", err)
		if err := utils.Sleep(ctx, r.cfg.DialInterval); err != nil {
			return err
		}
	}
	key := validator.auth.Key()
	for proxies := range r.proxies.Lock() {
		proxies[key] = client
	}
	<-ctx.Done()
	for proxies := range r.proxies.Lock() {
		if proxies[key] == client {
			delete(proxies, key)
		}
	}
	return ctx.Err()
}

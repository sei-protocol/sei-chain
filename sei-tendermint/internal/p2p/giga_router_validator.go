package p2p

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/time/rate"

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
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type gigaValidatorRouter struct {
	*GigaRouterCommon

	consensus *consensus.State
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
	producerState := producer.NewState(cfg.Producer, consensusState, cfg.App)
	validatorAddrs := newValidatorAddrs(dataState, cfg.AllowLocalAddrs)
	logger.Info("GigaRouter initialized (validator)", "validators", len(cfg.ValidatorAddrs), "dial_interval", cfg.DialInterval, "inbound_fullnode_cap", cfg.MaxInboundFullnodePeers, "persistent_peers", len(cfg.PersistentPeerIDs))
	return &gigaValidatorRouter{
		GigaRouterCommon: &GigaRouterCommon{
			cfg:             &cfg.GigaRouterCommonConfig,
			key:             key,
			data:            dataState,
			service:         giga.NewService(consensusState, validatorAddrs),
			poolIn:          giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
			poolInCommittee: giga.NewPool[atypes.PublicKey, rpc.Server[giga.API]](),
			poolOut:         giga.NewPool[atypes.PublicKey, rpc.Client[giga.API]](),
			proxies:         utils.NewRWMutex(map[atypes.PublicKey]*ethrpc.Client{}),
			signer:          signer,
			mempool:         utils.Some(producerState),
			validatorKey:    utils.Some(validatorKey),
			gigaSpec: gigaHandshakeSpec{
				ValidatorKey: utils.Some(cfg.ValidatorKey),
				EVMRPC:       cfg.EVMRPC,
			},
			selfAddr:       utils.Some(selfAddr),
			validatorAddrs: validatorAddrs,

			inboundFullnodeCount: utils.NewMutex(new(int64)),
			inboundFullnodeCap:   int64(cfg.MaxInboundFullnodePeers),
		},
		consensus: consensusState,
	}, nil
}

func (r *gigaValidatorRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnNamed("committeeMembers", func() error {
			return r.runPerCommitteeMember(ctx, false, r.runCommitteePeer)
		})
		s.SpawnNamed("publishValidatorAddr", func() error { return r.runPublishValidatorAddr(ctx) })
		s.SpawnNamed("consensus", func() error { return r.consensus.Run(ctx) })
		s.SpawnNamed("producer", func() error { return r.mempool.OrPanic("validator mempool").Run(ctx) })
		s.SpawnNamed("data", func() error { return r.data.Run(ctx) })
		s.SpawnNamed("pushAppHashes", func() error { return r.runPushAppHashes(ctx) })
		s.SpawnNamed("service", func() error { return r.service.Run(ctx) })
		return nil
	})
}

// runPublishValidatorAddr publishes this validator's signed address announcement each time it
// enters an Anchor epoch, and again whenever the table holds a higher version of it. Its entry is
// dropped from the table once it leaves, by runPerCommitteeMember.
func (r *gigaValidatorRouter) runPublishValidatorAddr(ctx context.Context) error {
	validatorKey := r.validatorKey.OrPanic("validator key")
	var version uint64
	for {
		if _, err := r.data.Anchor().Wait(ctx, func(a data.Anchor) bool {
			return inAnchorEpochs(a, validatorKey)
		}); err != nil {
			return err
		}
		for {
			// The version is the wall clock and is not persisted, so a restart may publish below the
			// version of the previous run. Peers hold that one and send it back, and the next
			// publish goes above it.
			version = max(uint64(time.Now().UnixNano()), version+1)
			// Read before the announcement is offered, so the wait below sees an Anchor change made during it.
			anchor := r.data.Anchor().Load()
			if !r.publishValidatorAddr(version) {
				// A refusal leaves the table unchanged, and re-entry does not change it either, so the retry
				// waits for the Anchor's epoch indices to change.
				indices := func(a data.Anchor) [2]atypes.EpochIndex {
					return [2]atypes.EpochIndex{a.Epochs[0].EpochIndex(), a.Epochs[1].EpochIndex()}
				}
				if _, err := r.data.Anchor().Wait(ctx, func(a data.Anchor) bool { return indices(a) != indices(anchor) }); err != nil {
					return err
				}
				break
			}
			superseding, err := r.awaitOwnAddrSuperseded(ctx, version)
			if err != nil {
				return err
			}
			if superseding == 0 {
				break
			}
			version = superseding
		}
	}
}

// awaitOwnAddrSuperseded waits until the table drops this validator's announcement or holds a version
// above published. It returns the held version, or 0 when the announcement was dropped and the caller
// should publish again from the Anchor.
func (r *gigaValidatorRouter) awaitOwnAddrSuperseded(ctx context.Context, published uint64) (uint64, error) {
	addrs := r.validatorAddrs.Watch()
	validatorKey := r.validatorKey.OrPanic("validator key")
	if _, err := addrs.Wait(ctx, func(held giga.ValidatorAddrSnapshot) bool {
		ann, ok := held[validatorKey]
		return !ok || ann.Msg().Version > published
	}); err != nil {
		return 0, err
	}
	ann, ok := addrs.Load()[validatorKey]
	if !ok {
		return 0, nil
	}
	return ann.Msg().Version, nil
}

// publishValidatorAddr signs this validator's address announcement at version and offers it
// to the table, which verifies it like any announcement from a peer. It reports whether the
// table stored the announcement.
func (r *gigaValidatorRouter) publishValidatorAddr(version uint64) bool {
	return r.validatorAddrs.Receive(r.signValidatorAddr(version))
}

// signValidatorAddr signs the announcement of this validator's dial address.
func (r *gigaValidatorRouter) signValidatorAddr(version uint64) *atypes.SignedValidatorAddrAnnouncement {
	self := r.selfAddr.OrPanic("validator router has no self address")
	return r.gigaSpec.ValidatorKey.OrPanic("validator router has no validator key").SignValidatorAddr(atypes.ValidatorAddrAnnouncement{
		Version:  version,
		NodeKey:  r.key.Public().String(),
		HostPort: tcp.HostPort{Hostname: self.Hostname, Port: self.Port}.String(),
	})
}

// runCommitteePeer maintains an outbound giga connection to a committee member.
// Self disables GetBlock: a loopback consumer always returns empty for missing
// catch-up heights and can starve the contiguous prefix while higher gap-fills
// keep retrying. Compare against the p2p node key (r.key.Public), not
// validatorKey (consensus signing key used for shard selection): GigaNodeAddr.Key is a
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

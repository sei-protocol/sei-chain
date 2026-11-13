package p2p

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	ablockstore "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// maxInboundFullnodePeers caps GigaRouterCommonConfig.MaxInboundFullnodePeers.
// Per-peer cost (~50-100 KB resident + ~8 goroutines + 1 fd) and NIC
// bandwidth bind well before this. Shard via an edge-fullnode tier if
// you need more.
const maxInboundFullnodePeers = 10000

var errGigaMembershipChanged = errors.New("inbound giga peer changed committee membership")

// errInboundFullnodeLimit is returned when a fullnode outside PersistentPeerIDs
// is refused because inboundFullnodeCap is already reached.
var errInboundFullnodeLimit = errors.New("inbound fullnode peer limit reached")

type gigaRouterCommon struct {
	cfg             *GigaRouterCommonConfig
	key             NodeSecretKey
	data            *data.State
	service         *giga.Service
	poolIn          *giga.Pool[NodePublicKey, rpc.Server[giga.API]]
	poolInCommittee *giga.Pool[atypes.PublicKey, rpc.Server[giga.API]]
	poolOut         *giga.Pool[atypes.PublicKey, rpc.Client[giga.API]]
	proxies         utils.RWMutex[map[atypes.PublicKey]*ethrpc.Client]
	app             *proxy.EVMProxy
	offer           utils.Option[handshakeOffer]
	selfAddr        utils.Option[NodeAddress]
	liveAddrs       utils.RWMutex[map[atypes.PublicKey]GigaNodeAddr]
	liveAddrVersion utils.AtomicSend[uint64]

	// inboundFullnodeCount tracks inbound connections currently served the
	// block-sync subset. Persistent and block-sync peers do not consume it.
	inboundFullnodeCount utils.Mutex[*int64]
	inboundFullnodeCap   int64
}

func (r *gigaRouterCommon) fillInboundHandshake(spec handshakeSpec) (handshakeSpec, utils.Option[handshakeOffer]) {
	spec.SeiGigaConnection = true
	if r.selfAddr.IsPresent() {
		spec.SelfAddr = r.selfAddr
	}
	return spec, r.offer
}

// BuildDataState validates the common config, constructs the committee, and
// returns an initialised data.State backed by blockStore.
//
// Whoever opened blockStore must close it after giga.Run returns, or immediately
// if construction of the GigaRouter fails. data.State never closes it.
func BuildDataState(cfg *GigaRouterCommonConfig, blockStore *ablockstore.Store) (*data.State, error) {
	if cfg.GenDoc.InitialHeight < 1 {
		return nil, fmt.Errorf("GenDoc.InitialHeight = %v, want >=1", cfg.GenDoc.InitialHeight)
	}
	if cfg.DialInterval <= 0 {
		return nil, fmt.Errorf("GigaRouterCommonConfig.DialInterval = %v, want > 0", cfg.DialInterval)
	}
	if cfg.MaxInboundFullnodePeers < 0 || cfg.MaxInboundFullnodePeers > maxInboundFullnodePeers {
		return nil, fmt.Errorf("GigaRouterCommonConfig.MaxInboundFullnodePeers = %v, want 0..%v", cfg.MaxInboundFullnodePeers, maxInboundFullnodePeers)
	}
	if cfg.PersistentStateDir == "" {
		return nil, errors.New("GigaRouterCommonConfig.PersistentStateDir is required")
	}
	if cfg.AppHashBuilder == nil {
		return nil, errors.New("GigaRouterCommonConfig.AppHashBuilder is required")
	}
	firstRoad := atypes.RoadIndex(cfg.GenDoc.InitialHeight) // nolint:gosec // verified to be positive.
	genesisWeights := map[atypes.PublicKey]uint64{}
	for k := range cfg.ValidatorAddrs {
		genesisWeights[k] = 1
	}
	genesisCommittee, err := atypes.NewCommittee(genesisWeights)
	if err != nil {
		return nil, fmt.Errorf("genesis committee: %w", err)
	}
	registry, err := epoch.NewRegistry(genesisCommittee, firstRoad, cfg.GenDoc.GenesisTime, utils.Some(cfg.PersistentStateDir))
	if err != nil {
		return nil, fmt.Errorf("epoch.NewRegistry(): %w", err)
	}
	ds, err := data.NewState(&data.Config{Registry: registry}, blockStore)
	if err != nil {
		return nil, fmt.Errorf("data.NewState: %w", err)
	}
	return ds, nil
}

func (r *gigaRouterCommon) LastCommittedBlockNumber() int64 {
	return r.app.LastBlockHeight()
}

// MaxGasEstimatedPerBlock reflects the network-wide block gas budget. Both
// roles ultimately resolve to genDoc.ConsensusParams.Block.MaxGas — the
// validator's producer.Config.MaxGasEstimatedPerBlock is also sourced from
// it at setup time, so read directly from genDoc here and skip the cache.
func (r *gigaRouterCommon) MaxGasEstimatedPerBlock() uint64 {
	return r.cfg.GenDoc.ConsensusParams.Block.MaxGasUint64()
}

// BlockByNumber returns the finalized SuperBlock at road n as a CometBFT ResultBlock.
// Its hash, height, timestamp, and transactions match the block passed to FinalizeBlock.
func (r *gigaRouterCommon) BlockByNumber(ctx context.Context, n atypes.RoadIndex) (*coretypes.ResultBlock, error) {
	gb, err := r.data.SuperBlock(ctx, n)
	if err != nil {
		// Map Autobahn's pruning sentinel to CometBFT's, so callers
		// (env.Block, evmrpc, ops tooling) get the same error type they
		// already handle on the CometBFT path. base is None because the
		// active lower bound lives in BlockStore's prune watermark (internal
		// to the store); both call sites format through the same helper.
		if errors.Is(err, atypes.ErrPruned) {
			return nil, coretypes.WrapErrHeightNotAvailable(utils.Clamp[int64](n), utils.None[int64]())
		}
		return nil, fmt.Errorf("data.SuperBlock(%v): %w", n, err)
	}
	return r.translateSuperBlock(gb), nil
}

// BlockByHash returns a finalized SuperBlock as a CometBFT ResultBlock.
// Unknown hashes return an empty ResultBlock with no error.
func (r *gigaRouterCommon) BlockByHash(ctx context.Context, hash common.Hash) (*coretypes.ResultBlock, error) {
	opt, err := r.data.SuperBlockByHash(atypes.RoadBlockHeaderHash(hash))
	if err != nil {
		return nil, fmt.Errorf("data.SuperBlockByHash: %w", err)
	}
	gb, ok := opt.Get()
	if !ok {
		return &coretypes.ResultBlock{}, nil
	}
	return r.translateSuperBlock(gb), nil
}

// translateSuperBlock converts a SuperBlock to the CometBFT result used by block RPCs.
// LastCommit has empty Signatures, matching executeBlock's empty CommitInfo.
func (r *gigaRouterCommon) translateSuperBlock(gb *atypes.SuperBlock) *coretypes.ResultBlock {
	srcTxs := gb.Txs
	tmTxs := make(types.Txs, len(srcTxs))
	for i, tx := range srcTxs {
		tmTxs[i] = tx
	}
	h := common.Hash(gb.Hash)
	return &coretypes.ResultBlock{
		BlockID: types.BlockID{Hash: tmbytes.HexBytes(h.Bytes())},
		Block: &types.Block{
			Header: types.Header{
				ChainID: r.cfg.GenDoc.ChainID,
				// Clamp accepts any constraints.Integer for From, so
				// gb.Height (a typed uint64) goes in directly — no
				// intermediate uint64() conversion needed.
				Height: utils.Clamp[int64](gb.Height),
				Time:   gb.Timestamp,
			},
			Data: types.Data{Txs: tmTxs},
			// Autobahn does not feed per-validator votes into the app. Filling N
			// absent signatures would make trace replay's BeginBlock bump
			// missed-block counters and diverge from production. ToReqBeginBlock
			// skips the per-validator loop when Signatures is empty, so empty
			// Votes flow into distribution/slashing on both paths.
			LastCommit: &types.Commit{},
		},
	}
}

// runPushAppHashes pushes the app hash of each executed block to the data layer,
// in road order from its next app proposal. It waits on the app hash builder for
// each one, so it never runs ahead of execution.
func (r *gigaRouterCommon) runPushAppHashes(ctx context.Context) error {
	for n := r.data.NextAppProposal(); ; n++ {
		entry, err := r.cfg.AppHashBuilder.GetAppHash(ctx, uint64(n))
		if err != nil {
			return fmt.Errorf("AppHashBuilder.GetAppHash(%v): %w", n, err)
		}
		// TODO: PushAppHash needs the validator set from right after block n.
		// GetValidators() is the app tip, which can be past n. This is exact only
		// while the app's validator set never changes, as for the EVM-only and mock
		// apps Autobahn runs. Read the next epoch's validator set from app state
		// once it keeps one.
		weights, err := committeeWeights(r.app.GetValidators())
		if err != nil {
			return err
		}
		appHash := entry.AppHash()
		if err := r.data.PushAppHash(ctx, n, appHash[:], weights); err != nil {
			return fmt.Errorf("r.data.PushAppHash(%v): %w", n, err)
		}
	}
}

// committeeWeights maps a bonded validator set to Autobahn voting power.
func committeeWeights(vals []proxy.ValidatorUpdate) (map[atypes.PublicKey]uint64, error) {
	weights := make(map[atypes.PublicKey]uint64, len(vals))
	for _, v := range vals {
		if v.Power <= 0 {
			continue
		}
		apk := v.PubKey
		if _, dup := weights[apk]; dup {
			return nil, fmt.Errorf("duplicate public key %s", apk)
		}
		power, ok := utils.SafeCast[uint64](v.Power)
		if !ok {
			return nil, fmt.Errorf("validator power %d does not fit uint64", v.Power)
		}
		weights[apk] = power
	}
	return weights, nil
}

// runEvmProxy maintains an EVM RPC client for one committee member.
func (r *gigaRouterCommon) runEvmProxy(ctx context.Context, validator atypes.PublicKey, addr GigaNodeAddr) error {
	for {
		client, err := ethrpc.DialContext(ctx, addr.EVMRPC.String())
		if err != nil {
			logger.Info("evm proxy dial failed", "url", addr.EVMRPC, "err", err)
			if err := utils.Sleep(ctx, r.cfg.DialInterval); err != nil {
				return err
			}
			continue
		}
		for proxies := range r.proxies.Lock() {
			proxies[validator] = client
		}
		<-ctx.Done()
		client.Close()
		for proxies := range r.proxies.Lock() {
			if proxies[validator] == client {
				delete(proxies, validator)
			}
		}
		return ctx.Err()
	}
}

// RestoreMarkerFile returns the path of the marker file that a state sync restore
// writes in the persistent state dir psd.
func RestoreMarkerFile(psd string) string {
	return filepath.Join(psd, "state-sync-restored")
}

// dialAndRunConn dials a peer, handshakes as a SeiGiga connection,
// registers the rpc client in poolOut, and runs runClient for the
// connection's lifetime. It verifies the p2p node key against the selected
// address, requires a validator claim for expectedValidatorKey, and registers
// the client under that validator.
func (r *gigaRouterCommon) dialAndRunConn(
	ctx context.Context,
	expectedValidatorKey atypes.PublicKey,
	expectedNodeKey NodePublicKey,
	hp tcp.HostPort,
	runClient func(ctx context.Context, client rpc.Client[giga.API]) error,
) error {
	addrs, err := hp.Resolve(ctx)
	if err != nil {
		return fmt.Errorf("%v.Resolve(): %w", hp, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%v.Resolve() = []", hp)
	}
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		tcpConn, err := tcp.Dial(ctx, addrs[0])
		if err != nil {
			return fmt.Errorf("tcp.Dial(%v): %w", addrs[0], err)
		}
		s.SpawnBg(func() error { return tcpConn.Run(ctx) })
		hConn, err := utils.WithOptTimeout1(ctx, r.cfg.HandshakeTimeout, func(ctx context.Context) (*handshakedConn, error) {
			return handshake(ctx, tcpConn, r.key, handshakeSpec{
				SelfAddr:          r.selfAddr,
				SeiGigaConnection: true,
			}, r.offer)
		})
		if err != nil {
			return fmt.Errorf("handshake(): %w", err)
		}
		if !hConn.msg.SeiGigaConnection {
			return fmt.Errorf("not a sei giga connection")
		}
		peerKey := hConn.msg.NodeAuth.Key()
		if peerKey != expectedNodeKey {
			return fmt.Errorf("peer node key = %v, want %v", peerKey, expectedNodeKey)
		}
		claim, ok := hConn.msg.GigaClaim.Get()
		if !ok {
			return fmt.Errorf("committee member %v: %w", expectedValidatorKey, errMissingGigaClaim)
		}
		if claim.Validator != expectedValidatorKey {
			return fmt.Errorf("peer validator key = %v, want %v", claim.Validator, expectedValidatorKey)
		}
		client := rpc.NewClient[giga.API]()
		return r.poolOut.InsertAndRun(ctx, expectedValidatorKey, client, func(ctx context.Context) error {
			return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
				s.Spawn(func() error { return client.Run(ctx, hConn.conn) })
				Global.gigaNewConnsAt("out").Add(1)
				Global.gigaConnsAt("out").Add(1)
				defer Global.gigaConnsAt("out").Add(-1)
				return runClient(ctx, client)
			})
		})
	})
}

// committeeMemberTask is work for one reachable committee member. It must run
// until ctx is cancelled; otherwise it is not restarted while the member stays
// in the committee.
type committeeMemberTask func(ctx context.Context, validator atypes.PublicKey, addr GigaNodeAddr) error

// memberSession is a committee member's cancellable task session.
type memberSession struct {
	cancel context.CancelFunc
	done   chan struct{}
	addr   GigaNodeAddr
}

// keepReplicas is the union of the commit and anchor committees.
func keepReplicas(anchor data.Anchor, commitEpoch *atypes.Epoch) map[atypes.PublicKey]struct{} {
	keep := map[atypes.PublicKey]struct{}{}
	for lane := range commitEpoch.Committee().Lanes().All() {
		keep[lane.Validator] = struct{}{}
	}
	for lane := range anchor.Epochs[0].Committee().Lanes().All() {
		keep[lane.Validator] = struct{}{}
	}
	return keep
}

// validatorAddr returns the address to dial validator at. The configured
// committee book wins; the live overlay only covers members it omits.
func (r *gigaRouterCommon) validatorAddr(validator atypes.PublicKey) (GigaNodeAddr, bool) {
	if addr, ok := r.cfg.ValidatorAddrs[validator]; ok {
		return addr, true
	}
	for addrs := range r.liveAddrs.RLock() {
		if addr, ok := addrs[validator]; ok {
			return addr, true
		}
	}
	return GigaNodeAddr{}, false
}

func sameGigaNodeAddr(a, b GigaNodeAddr) bool {
	return a.Key == b.Key && a.HostPort == b.HostPort && a.EVMRPC.String() == b.EVMRPC.String()
}

// acceptInbound returns the committee identity a verified inbound connection
// proved, recording the giga address it advertised. None means the peer is
// served the block-sync subset: it carried no claim, or it claimed a validator
// outside the current commit committee.
func (r *gigaRouterCommon) acceptInbound(hConn *handshakedConn) utils.Option[atypes.PublicKey] {
	claim, ok := hConn.msg.GigaClaim.Get()
	if !ok {
		return utils.None[atypes.PublicKey]()
	}
	if !r.data.Anchor().Load().NextIndexEpoch().Committee().HasReplica(claim.Validator) {
		return utils.None[atypes.PublicKey]()
	}
	selfAddr := hConn.msg.SelfAddr.OrPanic("verified giga claim has no SelfAddr")
	evmRPC := *utils.OrPanic1(url.Parse(claim.evmRPC))
	// If we will not use its EVMRPC, we learn none of the advertisement.
	if utils.IsLoopbackOrLinkLocalURL(evmRPC) {
		logger.Error("committee member advertised an unroutable EVM RPC; not learning its address",
			"validator", claim.Validator, "evmRPC", evmRPC.String())
		return utils.Some(claim.Validator)
	}
	addr := GigaNodeAddr{
		Key:      hConn.msg.NodeAuth.Key(),
		HostPort: tcp.HostPort{Hostname: selfAddr.Hostname, Port: selfAddr.Port},
		EVMRPC:   evmRPC,
	}
	for addrs := range r.liveAddrs.Lock() {
		if old, ok := addrs[claim.Validator]; ok && sameGigaNodeAddr(old, addr) {
			return utils.Some(claim.Validator)
		}
		addrs[claim.Validator] = addr
		r.liveAddrVersion.Store(r.liveAddrVersion.Load() + 1)
	}
	logger.Info("learned validator giga address", "validator", claim.Validator, "addr", addr)
	return utils.Some(claim.Validator)
}

// stopStaleSessions stops sessions for validators outside keepReplicas or
// dialing an address that is no longer current, and drops the overlay addresses
// of the departed. Departures are only acted on once Anchor is at most one
// epoch behind commitEpoch.
func (r *gigaRouterCommon) stopStaleSessions(
	ctx context.Context,
	live map[atypes.PublicKey]*memberSession,
	anchor data.Anchor,
	commitEpoch *atypes.Epoch,
) error {
	// Until Anchor is within one epoch, validators of the epochs in between are
	// in neither endpoint committee, and the AppQCs for those epochs cannot form
	// without them.
	settled := commitEpoch.EpochIndex() <= anchor.Epochs[0].EpochIndex()+1
	keep := keepReplicas(anchor, commitEpoch)
	if settled {
		for addrs := range r.liveAddrs.Lock() {
			dropped := false
			for validator := range addrs {
				if _, ok := keep[validator]; !ok {
					delete(addrs, validator)
					dropped = true
				}
			}
			if dropped {
				r.liveAddrVersion.Store(r.liveAddrVersion.Load() + 1)
			}
		}
	}
	var stale []*memberSession
	// Cancel every stale session before waiting for any of them.
	for validator, session := range live {
		if _, kept := keep[validator]; !kept {
			if !settled {
				continue
			}
		} else if addr, ok := r.validatorAddr(validator); ok && sameGigaNodeAddr(session.addr, addr) {
			continue
		}
		session.cancel()
		stale = append(stale, session)
		delete(live, validator)
	}
	for _, session := range stale {
		if _, _, err := utils.RecvOrClosed(ctx, session.done); err != nil {
			return err
		}
	}
	return nil
}

// runPerCommitteeMember runs tasks for each reachable member needed by the commit epoch or Anchor.
func (r *gigaRouterCommon) runPerCommitteeMember(ctx context.Context, tasks ...committeeMemberTask) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		live := map[atypes.PublicKey]*memberSession{}
		addrUpdates := r.liveAddrVersion.Subscribe()
		// End all sessions before the scope waits for them.
		defer func() {
			for _, session := range live {
				session.cancel()
			}
		}()
		// Anchor is republished on every persist batch; only its epoch can
		// change the keep set.
		epochOf := func(a data.Anchor) atypes.EpochIndex { return a.Epochs[0].EpochIndex() }

		for ctx.Err() == nil {
			addrVersion := addrUpdates.Load()
			anchor := r.data.Anchor().Load()
			commitEpoch := anchor.NextIndexEpoch()
			if err := r.stopStaleSessions(ctx, live, anchor, commitEpoch); err != nil {
				return err
			}
			for validator := range keepReplicas(anchor, commitEpoch) {
				if _, ok := live[validator]; ok {
					continue
				}
				addr, ok := r.validatorAddr(validator)
				if !ok {
					logger.Error("committee member has no configured address; not dialing", "validator", validator)
					continue
				}
				taskCtx, cancel := context.WithCancel(ctx)
				done := make(chan struct{})
				live[validator] = &memberSession{cancel: cancel, done: done, addr: addr}
				s.SpawnNamed(addr.String(), func() error {
					defer close(done)
					return utils.IgnoreCancel(scope.Run(taskCtx, func(ctx context.Context, ms scope.Scope) error {
						for _, task := range tasks {
							ms.Spawn(func() error { return task(ctx, validator, addr) })
						}
						return nil
					}))
				})
			}
			if err := utils.WaitAny(ctx, func() bool {
				return r.data.Anchor().Load().NextIndexEpoch().EpochIndex() != commitEpoch.EpochIndex() ||
					epochOf(r.data.Anchor().Load()) != epochOf(anchor) ||
					addrUpdates.Load() != addrVersion
			}, r.data.Anchor(), addrUpdates); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
}

// runUntilMembershipChange runs f until validator leaves the current commit
// committee. It reports whether a leave ended f.
func (r *gigaRouterCommon) runUntilMembershipChange(
	ctx context.Context,
	validator atypes.PublicKey,
	f func(ctx context.Context) error,
) (changed bool, err error) {
	err = scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			_, err := r.data.Anchor().Wait(ctx, func(anchor data.Anchor) bool {
				return !anchor.NextIndexEpoch().Committee().HasReplica(validator)
			})
			if err != nil {
				return err
			}
			changed = true
			s.Cancel(nil)
			return nil
		})
		return f(ctx)
	})
	return changed, utils.IgnoreCancel(err)
}

// RunInboundConn serves an inbound giga connection. A peer proving current
// commit-committee membership is served as a validator; every other peer is
// served as a fullnode for the life of this socket.
func (r *gigaRouterCommon) RunInboundConn(ctx context.Context, hConn *handshakedConn) error {
	if !hConn.msg.SeiGigaConnection {
		return fmt.Errorf("not a SeiGiga connection")
	}
	if member, ok := r.acceptInbound(hConn).Get(); ok {
		return r.runInboundValidator(ctx, hConn, member)
	}
	// A member who inbounds before they appear in our anchor's first commit epoch is
	// served as a fullnode for this socket's life; their own dialer redials
	// after DialInterval and re-handshakes into the validator role.
	return r.runInboundFullnode(ctx, hConn)
}

func (r *gigaRouterCommon) runInboundFullnode(ctx context.Context, hConn *handshakedConn) error {
	key := hConn.msg.NodeAuth.Key()
	server := rpc.NewServer[giga.API]()
	// Acquired before InsertAndRun, which evicts any live connection for this key.
	// A persistent or block-sync peer does not consume the cap.
	if _, skip := r.cfg.PersistentPeerIDs[key.NodeID()]; !skip {
		for count := range r.inboundFullnodeCount.Lock() {
			if *count >= r.inboundFullnodeCap {
				return fmt.Errorf("%w (%d)", errInboundFullnodeLimit, r.inboundFullnodeCap)
			}
			*count++
		}
		defer func() {
			for count := range r.inboundFullnodeCount.Lock() {
				*count--
			}
		}()
	}
	return r.poolIn.InsertAndRun(ctx, key, server, func(ctx context.Context) error {
		return r.runInboundMux(ctx, server, hConn, func(ctx context.Context) error {
			return r.service.RunServer(ctx, server, false)
		})
	})
}

func (r *gigaRouterCommon) runInboundValidator(ctx context.Context, hConn *handshakedConn, member atypes.PublicKey) error {
	key := hConn.msg.NodeAuth.Key()
	server := rpc.NewServer[giga.API]()
	return r.poolInCommittee.InsertAndRun(ctx, member, server, func(ctx context.Context) error {
		return r.runInboundMux(ctx, server, hConn, func(ctx context.Context) error {
			changed, err := r.runUntilMembershipChange(ctx, member, func(ctx context.Context) error {
				return r.service.RunServer(ctx, server, true)
			})
			if err != nil {
				return err
			}
			if changed {
				logger.Info("inbound giga peer left the committee; closing", "validator", member, "addr", key)
				return errGigaMembershipChanged
			}
			return nil
		})
	})
}

func (r *gigaRouterCommon) runInboundMux(
	ctx context.Context,
	server rpc.Server[giga.API],
	hConn *handshakedConn,
	serve func(ctx context.Context) error,
) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// Background: a membership change must cancel the mux. Spawn would
		// keep this scope alive until the peer closes the socket.
		s.SpawnBg(func() error { return server.Run(ctx, hConn.conn) })
		Global.gigaNewConnsAt("in").Add(1)
		Global.gigaConnsAt("in").Add(1)
		defer Global.gigaConnsAt("in").Add(-1)
		if err := serve(ctx); err != nil {
			return fmt.Errorf("inbound from %v: %w", hConn.msg.NodeAuth.Key(), err)
		}
		return nil
	})
}

// Validators returns the Autobahn validator set that certified global height n.
// Before the first CommitQC, the genesis road resolves to the genesis committee.
func (r *gigaRouterCommon) Validators(n atypes.RoadIndex) ([]*types.Validator, error) {
	qc, err := r.data.TryQC(n)
	anchor := r.data.Anchor().Load()
	if err != nil {
		if errors.Is(err, atypes.ErrNotFound) {
			if genesis, ok := anchor.EpochAt(n).Get(); ok && genesis.EpochIndex() == 0 && n == genesis.RoadRange().First {
				return committeeValidators(genesis.Committee())
			}
		}
		return nil, heightLookupError(n, err)
	}
	ep, ok := anchor.EpochAt(qc.Index()).Get()
	if !ok {
		return nil, heightLookupError(n, atypes.ErrPruned)
	}
	return committeeValidators(ep.Committee())
}

// heightLookupError returns the RPC error for a data lookup at global height n.
func heightLookupError(n atypes.RoadIndex, err error) error {
	switch {
	case errors.Is(err, atypes.ErrPruned):
		return coretypes.WrapErrHeightNotAvailable(utils.Clamp[int64](n), utils.None[int64]())
	case errors.Is(err, atypes.ErrNotFound):
		return fmt.Errorf("%w (requested height: %d)", coretypes.ErrHeightExceedsChainHead, utils.Clamp[int64](n))
	default:
		return err
	}
}

func committeeValidators(committee *atypes.Committee) ([]*types.Validator, error) {
	vs := make([]*types.Validator, 0, committee.Lanes().Len())
	for lane := range committee.Lanes().All() {
		power, ok := utils.SafeCast[int64](committee.Weight(lane.Validator))
		if !ok {
			return nil, fmt.Errorf("committee member %v: weight %d does not fit int64", lane.Validator, committee.Weight(lane.Validator))
		}
		vs = append(vs, types.NewValidator(lane.Validator.ED25519(), power))
	}
	sort.Sort(types.ValidatorsByVotingPower(vs))
	return vs, nil
}

// EvmProxy returns the shard owner's EVMRPC client for an EVM tx sender, or
// None if the caller should handle it locally. Overridden on
// *gigaValidatorRouter to short-circuit self-shard sends.
func (r *gigaRouterCommon) evmProxy(validator atypes.PublicKey) utils.Option[*ethrpc.Client] {
	for proxies := range r.proxies.RLock() {
		client, ok := proxies[validator]
		if ok {
			return utils.Some(client)
		}
	}
	return utils.None[*ethrpc.Client]()
}

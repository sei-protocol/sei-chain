package p2p

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	ablockstore "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
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
	app             *abevm.Proxy
	signer          *abevm.Signer
	gigaSpec        gigaHandshakeSpec
	selfAddr        utils.Option[NodeAddress]

	// inboundFullnodeCount tracks inbound connections currently served the
	// block-sync subset. Persistent and block-sync peers do not consume it.
	inboundFullnodeCount utils.Mutex[*int64]
	inboundFullnodeCap   int64
}

// CheckTx checks a transaction for admission without inserting it into the mempool.
func (r *gigaRouterCommon) CheckTx(raw []byte) (abevm.EvmTx, abevm.AdmittedTx, error) {
	return abevm.CheckTx(r.app, r.signer, raw)
}

// RememberEvmSender records a verified sender for local admission.
func (r *gigaRouterCommon) RememberEvmSender(hash common.Hash, sender common.Address) {
	r.signer.Remember(hash, sender)
}

// ForgetEvmSender removes the cached sender for hash.
func (r *gigaRouterCommon) ForgetEvmSender(hash common.Hash) {
	r.signer.Forget(hash)
}

func (r *gigaRouterCommon) fillInboundHandshake(spec handshakeSpec) handshakeSpec {
	spec.Giga = utils.Some(r.gigaSpec)
	if r.selfAddr.IsPresent() {
		spec.SelfAddr = r.selfAddr
	}
	return spec
}

// genesisCommittee returns the committee epochs 0 and 1 start from: every
// address-book validator with weight 1.
func genesisCommittee(validatorAddrs map[atypes.PublicKey]GigaNodeAddr) (*atypes.Committee, error) {
	weights := make(map[atypes.PublicKey]uint64, len(validatorAddrs))
	for k := range validatorAddrs {
		weights[k] = 1
	}
	committee, err := atypes.NewCommittee(weights)
	if err != nil {
		return nil, fmt.Errorf("genesis committee: %w", err)
	}
	return committee, nil
}

// BuildDataState validates the common config and returns an initialised data.State backed by
// blockStore. app supplies the epoch window, and genesisTimestamp is the
// timestamp of the first proposal.
//
// Whoever opened blockStore must close it after giga.Run returns, or immediately
// if construction of the GigaRouter fails. data.State never closes it.
func BuildDataState(
	cfg *GigaRouterCommonConfig,
	blockStore *ablockstore.Store,
	app *abevm.Proxy,
	genesisTimestamp time.Time,
) (*data.State, error) {
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
	ds, err := data.NewState(&data.Config{App: app, GenesisTimestamp: genesisTimestamp}, blockStore)
	if err != nil {
		return nil, fmt.Errorf("data.NewState: %w", err)
	}
	return ds, nil
}

func (r *gigaRouterCommon) LastCommittedHeight() int64 {
	return r.app.LastBlockHeight()
}

// MaxGasEstimatedPerBlock reflects the network-wide block gas budget. Both
// roles ultimately resolve to genDoc.ConsensusParams.Block.MaxGas — the
// validator's producer.Config.MaxGasEstimatedPerBlock is also sourced from
// it at setup time, so read directly from genDoc here and skip the cache.
func (r *gigaRouterCommon) MaxGasEstimatedPerBlock() uint64 {
	return r.cfg.GenDoc.ConsensusParams.Block.MaxGasUint64()
}

// BlockByNumber returns the finalized EVM block at road n.
func (r *gigaRouterCommon) BlockByNumber(ctx context.Context, n atypes.RoadHeight) (*abevm.EvmBlock, error) {
	block, err := r.data.SuperBlock(ctx, n)
	if err != nil {
		// Keep the RPC pruning sentinel for callers that distinguish pruned heights.
		// The active lower bound lives in the block store's prune watermark.
		if errors.Is(err, atypes.ErrPruned) {
			return nil, coretypes.WrapErrHeightNotAvailable(utils.Clamp[int64](n), utils.None[int64]())
		}
		return nil, fmt.Errorf("data.SuperBlock(%v): %w", n, err)
	}
	return r.evmBlock(block), nil
}

// BlockByHash returns the finalized EVM block with hash, or None when unknown.
func (r *gigaRouterCommon) BlockByHash(ctx context.Context, hash common.Hash) (utils.Option[*abevm.EvmBlock], error) {
	block, err := r.data.SuperBlockByHash(atypes.RoadBlockHeaderHash(hash))
	if err != nil {
		return utils.None[*abevm.EvmBlock](), fmt.Errorf("data.SuperBlockByHash: %w", err)
	}
	return block.Map(r.evmBlock), nil
}

func (r *gigaRouterCommon) evmBlock(block *atypes.SuperBlock) *abevm.EvmBlock {
	return &abevm.EvmBlock{
		Hash:    common.Hash(block.Hash),
		Height:  block.Height,
		Time:    block.Timestamp,
		Txs:     block.Txs,
		ChainID: r.cfg.GenDoc.ChainID,
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
		appHash := entry.AppHash()
		if err := r.data.PushAppHash(ctx, n, appHash[:]); err != nil {
			return fmt.Errorf("r.data.PushAppHash(%v): %w", n, err)
		}
	}
}

// runEvmProxy maintains an EVM RPC client for the lifetime of its Giga connection.
func (r *gigaRouterCommon) runEvmProxy(ctx context.Context, validator validatorHandshake) error {
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

// RestoreMarkerFile returns the path of the marker file that a state sync restore
// writes in the persistent state dir psd.
func RestoreMarkerFile(psd string) string {
	return filepath.Join(psd, "state-sync-restore-pending")
}

// RestoreProofFile returns the path of the state sync proof file of the checkpoint that a
// state sync restore installs in the persistent state dir psd.
func RestoreProofFile(psd string) string {
	return filepath.Join(psd, "state-sync-proof")
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
			return handshake(ctx, tcpConn, handshakeSpec{
				NodeKey:  r.key,
				IsServer: false,
				SelfAddr: r.selfAddr,
				Giga:     utils.Some(r.gigaSpec),
			})
		})
		if err != nil {
			return fmt.Errorf("handshake(): %w", err)
		}
		gigaHS, ok := hConn.msg.Giga.Get()
		if !ok {
			return fmt.Errorf("not a sei giga connection")
		}
		peerKey := hConn.msg.NodeAuth.Key()
		if peerKey != expectedNodeKey {
			return fmt.Errorf("peer node key = %v, want %v", peerKey, expectedNodeKey)
		}
		validator, ok := gigaHS.Validator.Get()
		if !ok {
			return fmt.Errorf("committee member %v: %w", expectedValidatorKey, errMissingValidatorAuth)
		}
		if validator.auth.Key() != expectedValidatorKey {
			return fmt.Errorf("peer validator key = %v, want %v", validator.auth.Key(), expectedValidatorKey)
		}
		client := rpc.NewClient[giga.API]()
		return r.poolOut.InsertAndRun(ctx, expectedValidatorKey, client, func(ctx context.Context) error {
			return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
				s.Spawn(func() error { return client.Run(ctx, hConn.conn) })
				s.Spawn(func() error { return r.runEvmProxy(ctx, validator) })
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

// validatorAddr returns the configured address for a validator.
func (r *gigaRouterCommon) validatorAddr(validator atypes.PublicKey) (GigaNodeAddr, bool) {
	addr, ok := r.cfg.ValidatorAddrs[validator]
	return addr, ok
}

// inAnchorEpochs reports whether validator is a member of an epoch Anchor holds.
func inAnchorEpochs(anchor data.Anchor, validator atypes.PublicKey) bool {
	for _, ep := range anchor.Epochs {
		if ep.Committee().HasReplica(validator) {
			return true
		}
	}
	return false
}

// stopStaleSessions stops sessions for validators outside epochs or dialing an
// address that is no longer current.
func (r *gigaRouterCommon) stopStaleSessions(
	ctx context.Context,
	live map[atypes.PublicKey]*memberSession,
	epochs []*atypes.Epoch,
) error {
	kept := func(validator atypes.PublicKey) bool {
		for _, ep := range epochs {
			if ep.Committee().HasReplica(validator) {
				return true
			}
		}
		return false
	}
	var stale []*memberSession
	// Cancel every stale session before waiting for any of them.
	for validator, session := range live {
		if kept(validator) {
			if addr, ok := r.validatorAddr(validator); ok && session.addr == addr {
				continue
			}
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

// runPerCommitteeMember runs tasks for each reachable member of the Anchor
// epochs, or of the commit epoch when currentCommitteeOnly is set.
func (r *gigaRouterCommon) runPerCommitteeMember(ctx context.Context, currentCommitteeOnly bool, tasks ...committeeMemberTask) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		live := map[atypes.PublicKey]*memberSession{}
		// End all sessions before the scope waits for them.
		defer func() {
			for _, session := range live {
				session.cancel()
			}
		}()
		// Anchor is republished on every persist batch, so the wait compares
		// the epoch indices it depends on rather than the Anchor value.
		epochOf := func(a data.Anchor) atypes.EpochIndex { return a.Epochs[0].EpochIndex() }

		for ctx.Err() == nil {
			anchor := r.data.Anchor().Load()
			commitEpoch := anchor.NextHeightEpoch()
			// A member of Epochs[0] may hold the only copy of a lane block, or of
			// the headers a CommitQC needs. Epochs[1] is the next committee while
			// the commit epoch is Epochs[0].
			epochs := anchor.Epochs[:]
			if currentCommitteeOnly {
				epochs = []*atypes.Epoch{commitEpoch}
			}
			if err := r.stopStaleSessions(ctx, live, epochs); err != nil {
				return err
			}
			for _, ep := range epochs {
				for lane := range ep.Committee().Lanes().All() {
					validator := lane.Validator
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
			}
			if err := utils.WaitAny(ctx, func() bool {
				return r.data.Anchor().Load().NextHeightEpoch().EpochIndex() != commitEpoch.EpochIndex() ||
					epochOf(r.data.Anchor().Load()) != epochOf(anchor)
			}, r.data.Anchor()); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
}

// runUntilMembershipChange runs f until validator leaves all Anchor epochs.
// It returns errGigaMembershipChanged when a leave ends f.
func (r *gigaRouterCommon) runUntilMembershipChange(
	ctx context.Context,
	validator atypes.PublicKey,
	f func(ctx context.Context) error,
) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			if _, err := r.data.Anchor().Wait(ctx, func(anchor data.Anchor) bool {
				return !inAnchorEpochs(anchor, validator)
			}); err != nil {
				return utils.IgnoreCancel(err)
			}
			return errGigaMembershipChanged
		})
		return f(ctx)
	})
}

// RunInboundConn serves an inbound giga connection. A peer proving membership
// of an Anchor epoch is served as a validator; every other peer is served
// as a fullnode for the life of this socket.
func (r *gigaRouterCommon) RunInboundConn(ctx context.Context, hConn *handshakedConn) error {
	gigaHS, ok := hConn.msg.Giga.Get()
	if !ok {
		return fmt.Errorf("not a SeiGiga connection")
	}
	if validator, ok := gigaHS.Validator.Get(); ok && inAnchorEpochs(r.data.Anchor().Load(), validator.auth.Key()) {
		return r.runInboundValidator(ctx, hConn, validator)
	}
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

func (r *gigaRouterCommon) runInboundValidator(ctx context.Context, hConn *handshakedConn, validator validatorHandshake) error {
	key := hConn.msg.NodeAuth.Key()
	member := validator.auth.Key()
	server := rpc.NewServer[giga.API]()
	if err := r.poolInCommittee.InsertAndRun(ctx, member, server, func(ctx context.Context) error {
		return r.runUntilMembershipChange(ctx, member, func(ctx context.Context) error {
			return r.runInboundMux(ctx, server, hConn, func(ctx context.Context) error {
				return r.service.RunServer(ctx, server, true)
			})
		})
	}); err != nil {
		return fmt.Errorf("validator %s, node %s: %w", member, key, err)
	}
	return nil
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
func (r *gigaRouterCommon) Validators(n atypes.RoadHeight) ([]*types.Validator, error) {
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
	ep, ok := anchor.EpochAt(qc.Height()).Get()
	if !ok {
		return nil, heightLookupError(n, atypes.ErrPruned)
	}
	return committeeValidators(ep.Committee())
}

// heightLookupError returns the RPC error for a data lookup at global height n.
func heightLookupError(n atypes.RoadHeight, err error) error {
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

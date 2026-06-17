package p2p

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
)

type GigaNodeAddr struct {
	Key      NodePublicKey
	HostPort tcp.HostPort
	EVMRPC   utils.Option[*url.URL]
}

func (a GigaNodeAddr) String() string {
	return fmt.Sprintf("%v@%v", a.Key, a.HostPort)
}

// GigaRouterCommonConfig is the slice of giga config shared by both
// validator and fullnode constructors.
type GigaRouterCommonConfig struct {
	// DialInterval is the outbound dial-retry sleep on both paths and the
	// initial backoff on the fullnode subscriber. Must be > 0.
	DialInterval time.Duration
	// ValidatorAddrs is the committee table. Every entry must carry a
	// non-None EVMRPC URL — EvmProxy's silent-drop branch is otherwise
	// reachable. Empty maps are rejected at construction.
	ValidatorAddrs map[atypes.PublicKey]GigaNodeAddr
	// PersistentStateDir is the on-disk root for the data WAL (and the
	// validator's consensus persister in a sibling subdir). None ⇒ in-memory.
	PersistentStateDir utils.Option[string]
	// App is the ABCI proxy executeBlock drives. NewGigaValidatorRouter
	// also copies this into cfg.Producer.App so the producer's internal
	// mempool drives the same proxy.
	App    *proxy.Proxy
	GenDoc *types.GenesisDoc
}

// GigaFullnodeConfig configures a non-validator GigaRouter (pulls
// finalized blocks from committee members, executes locally, forwards EVM
// writes to the shard owner via EvmProxy — no consensus, no producer).
type GigaFullnodeConfig struct {
	GigaRouterCommonConfig
}

// GigaValidatorConfig configures a committee-member GigaRouter.
type GigaValidatorConfig struct {
	GigaRouterCommonConfig
	ValidatorKey atypes.SecretKey
	ViewTimeout  func(atypes.View) time.Duration
	// Producer carries gas/tx-per-block caps, block interval, etc. App
	// is filled by NewGigaValidatorRouter from common.App — leave nil.
	Producer *producer.Config
	// MaxInboundFullnodePeers caps inbound block-sync connections from
	// non-committee peers. 0 rejects all; n > 0 caps at n; negative is
	// rejected. setup.go fills the operator-facing default
	// (config.DefaultAutobahnMaxInboundFullnodePeers) when the TOML key
	// is absent.
	MaxInboundFullnodePeers int
}

// GigaRouter is the per-node entry into Autobahn — the read-path / Run /
// EvmProxy surface external callers (router.go,
// internal/rpc/core/{blocks,status,mempool}.go) reach through. Two
// concrete impls in this package: *gigaValidatorRouter (committee members)
// and *gigaFullnodeRouter. Validator-only operations are reached via
// AsValidator() — see GigaValidatorRouter. RunInboundConn lives on
// *gigaValidatorRouter directly (fullnodes don't accept inbound, so
// putting it on GigaRouter would force a dummy fullnode impl).
type GigaRouter interface {
	Run(ctx context.Context) error
	LastCommittedBlockNumber() int64
	MaxGasEstimatedPerBlock() uint64
	BlockByNumber(ctx context.Context, n atypes.GlobalBlockNumber) (*coretypes.ResultBlock, error)
	BlockByHash(ctx context.Context, hash atypes.BlockHeaderHash) (*coretypes.ResultBlock, error)
	EvmProxy(sender common.Address) (*url.URL, bool)
	// AsValidator returns Some on validators, None on fullnodes. Callers
	// that need the producer-backed mempool branch on Get().
	AsValidator() utils.Option[GigaValidatorRouter]
}

// GigaValidatorRouter is the validator-only surface reached via
// GigaRouter.AsValidator(). Exposes the producer-backed mempool that the
// RPC layer (broadcast_tx_*, evm tx insertion, nonce lookup) drives.
type GigaValidatorRouter interface {
	Mempool() *producer.State
}

type gigaRouterCommon struct {
	cfg     *GigaRouterCommonConfig
	key     NodeSecretKey
	data    *data.State
	service *giga.Service
	poolOut *giga.Pool[NodePublicKey, rpc.Client[giga.API]]
	app     *proxy.Proxy
	// validatorKey is Some on validators (own committee key). Read by
	// EvmProxy to short-circuit self-shard sends to local mempool instead
	// of HTTP-forwarding to ourselves.
	validatorKey utils.Option[atypes.PublicKey]
	// lastExecutedBlock is the height the app has Commit-ed. Read by
	// LastCommittedBlockNumber so /status reports the executed frontier
	// (matches CometBFT — clients querying receipts at the reported height
	// won't see a height the app hasn't reached). Seeded from
	// app.Info().LastBlockHeight at startup; advanced after every Commit.
	lastExecutedBlock atomic.Int64
}

type gigaValidatorRouter struct {
	*gigaRouterCommon

	consensus      *consensus.State
	producer       *producer.State
	producerConfig *producer.Config
	poolIn         *giga.Pool[NodePublicKey, rpc.Server[giga.API]]

	// inboundFullnodeCount tracks live non-committee inbound block-sync
	// connections. Acquire is Add(1) + compare against cap; release is
	// Add(-1). Brief overshoot under contention over-rejects by one or
	// two peers but never over-accepts — and the atomic avoids queueing.
	inboundFullnodeCount atomic.Int32
	inboundFullnodeCap   int32
}

// validateCommonAndBuildData runs the validation and data-layer setup
// shared by both giga constructors. The every-committee-member-has-an-
// EVMRPC-URL guard lives here so EvmProxy's silent-drop branch is
// unreachable in production on either mode.
//
// TODO(autobahn): once sei-db/ledger_db/block.BlockDB has a writer wired
// (see BlockByNumber's TODO), the data WAL is redundant — drop the
// directory and make this a no-op.
func validateCommonAndBuildData(cfg *GigaRouterCommonConfig) (*data.State, error) {
	if cfg.GenDoc == nil {
		return nil, fmt.Errorf("GigaRouterCommonConfig.GenDoc must be set")
	}
	if cfg.GenDoc.InitialHeight < 1 {
		return nil, fmt.Errorf("GenDoc.InitialHeight = %v, want >=1", cfg.GenDoc.InitialHeight)
	}
	if cfg.DialInterval <= 0 {
		return nil, fmt.Errorf("GigaRouterCommonConfig.DialInterval = %v, want > 0", cfg.DialInterval)
	}
	if cfg.App == nil {
		return nil, fmt.Errorf("GigaRouterCommonConfig.App must be set")
	}
	// Guards runFullnodeSubscriber's `(i + 1) % len(addrs)` against
	// direct construction that bypasses AutobahnFileConfig.Validate.
	if len(cfg.ValidatorAddrs) == 0 {
		return nil, fmt.Errorf("GigaRouterCommonConfig.ValidatorAddrs is empty; need at least one committee member")
	}
	for vk, addr := range cfg.ValidatorAddrs {
		if !addr.EVMRPC.IsPresent() {
			return nil, fmt.Errorf("autobahn: validator %s is missing evmrpc URL; every committee member must expose one so EvmProxy can forward", vk)
		}
	}
	committee, err := atypes.NewRoundRobinElection(
		slices.Collect(maps.Keys(cfg.ValidatorAddrs)),
		atypes.GlobalBlockNumber(cfg.GenDoc.InitialHeight), // nolint:gosec // verified to be positive.
		cfg.GenDoc.GenesisTime,
	)
	if err != nil {
		return nil, fmt.Errorf("atypes.NewRoundRobinElection(): %w", err)
	}
	dataWAL, err := data.NewDataWAL(cfg.PersistentStateDir, committee)
	if err != nil {
		return nil, fmt.Errorf("data.NewDataWAL(): %w", err)
	}
	dataState, err := data.NewState(&data.Config{Committee: committee}, dataWAL)
	if err != nil {
		return nil, fmt.Errorf("data.NewState(): %w", err)
	}
	return dataState, nil
}

func NewGigaFullnodeRouter(cfg *GigaFullnodeConfig, key NodeSecretKey) (*gigaFullnodeRouter, error) {
	dataState, err := validateCommonAndBuildData(&cfg.GigaRouterCommonConfig)
	if err != nil {
		return nil, err
	}
	logger.Info("GigaRouter initialized (fullnode)", "validators", len(cfg.ValidatorAddrs))
	return &gigaFullnodeRouter{
		gigaRouterCommon: &gigaRouterCommon{
			cfg:          &cfg.GigaRouterCommonConfig,
			key:          key,
			data:         dataState,
			service:      giga.NewBlockSyncService(dataState),
			poolOut:      giga.NewPool[NodePublicKey, rpc.Client[giga.API]](),
			app:          cfg.App,
			validatorKey: utils.None[atypes.PublicKey](),
		},
	}, nil
}

// NewGigaValidatorRouter returns the concrete validator type (not the
// interface) so router.go can reach RunInboundConn in-package without a
// runtime downcast.
func NewGigaValidatorRouter(cfg *GigaValidatorConfig, key NodeSecretKey) (*gigaValidatorRouter, error) {
	dataState, err := validateCommonAndBuildData(&cfg.GigaRouterCommonConfig)
	if err != nil {
		return nil, err
	}
	if cfg.Producer == nil {
		return nil, fmt.Errorf("GigaValidatorConfig.Producer must be set")
	}
	if cfg.ViewTimeout == nil {
		return nil, fmt.Errorf("GigaValidatorConfig.ViewTimeout must be set")
	}
	// One App per node — common owns it; mirror into producer.Config so
	// the producer's internal mempool drives the same ABCI proxy.
	cfg.Producer.App = cfg.App
	consensusState, err := consensus.NewState(&consensus.Config{
		Key:                cfg.ValidatorKey,
		ViewTimeout:        cfg.ViewTimeout,
		PersistentStateDir: cfg.PersistentStateDir,
	}, dataState)
	if err != nil {
		return nil, fmt.Errorf("consensus.NewState(): %w", err)
	}
	producerState := producer.NewState(cfg.Producer, consensusState)
	if cfg.MaxInboundFullnodePeers < 0 {
		return nil, fmt.Errorf("GigaValidatorConfig.MaxInboundFullnodePeers = %v, want >= 0", cfg.MaxInboundFullnodePeers)
	}
	logger.Info("GigaRouter initialized", "validators", len(cfg.ValidatorAddrs), "dial_interval", cfg.DialInterval, "inbound_fullnode_cap", cfg.MaxInboundFullnodePeers)
	return &gigaValidatorRouter{
		gigaRouterCommon: &gigaRouterCommon{
			cfg:          &cfg.GigaRouterCommonConfig,
			key:          key,
			data:         dataState,
			service:      giga.NewService(consensusState),
			poolOut:      giga.NewPool[NodePublicKey, rpc.Client[giga.API]](),
			app:          cfg.App,
			validatorKey: utils.Some(cfg.ValidatorKey.Public()),
		},
		consensus:          consensusState,
		producer:           producerState,
		producerConfig:     cfg.Producer,
		poolIn:             giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
		inboundFullnodeCap: int32(cfg.MaxInboundFullnodePeers), // nolint:gosec // validated >= 0 above.
	}, nil
}

func (r *gigaRouterCommon) LastCommittedBlockNumber() int64 {
	return r.lastExecutedBlock.Load()
}

func (r *gigaValidatorRouter) MaxGasEstimatedPerBlock() uint64 {
	return r.producerConfig.MaxGasEstimatedPerBlock
}

func (r *gigaValidatorRouter) AsValidator() utils.Option[GigaValidatorRouter] {
	return utils.Some[GigaValidatorRouter](r)
}

func (r *gigaValidatorRouter) Mempool() *producer.State {
	return r.producer
}

// BlockByNumber and BlockByHash translate Autobahn's GlobalBlock to the
// CometBFT coretypes.ResultBlock shape so evmrpc (which wraps receipts /
// logs with block context) keeps working without CometBFT's BlockStore
// being populated. Block.Header.{ChainID,Height,Time} and Block.Data.Txs
// are populated; the rest (AppHash, ProposerAddress, LastCommit, …) stay
// zero — evmrpc doesn't read them on the receipt path.
//
// TODO(autobahn): switch both to sei-db/ledger_db/block.BlockDB once a
// writer is wired from executeBlock; data.State's index goes away then.
func (r *gigaRouterCommon) BlockByNumber(ctx context.Context, n atypes.GlobalBlockNumber) (*coretypes.ResultBlock, error) {
	gb, err := r.data.GlobalBlock(ctx, n)
	if err != nil {
		// Map Autobahn's pruning sentinel to CometBFT's so callers
		// (env.Block, evmrpc, ops tooling) hit the same error type they
		// already handle on the CometBFT path.
		if errors.Is(err, data.ErrPruned) {
			return nil, coretypes.WrapErrHeightNotAvailable(utils.Clamp[int64](n), utils.None[int64]())
		}
		return nil, fmt.Errorf("data.GlobalBlock(%v): %w", n, err)
	}
	return r.translateGlobalBlock(gb), nil
}

// BlockByHash matches CometBFT semantics for unknown hashes:
// &ResultBlock{Block: nil} with no error.
func (r *gigaRouterCommon) BlockByHash(ctx context.Context, hash atypes.BlockHeaderHash) (*coretypes.ResultBlock, error) {
	opt, err := r.data.GlobalBlockByHash(hash)
	if err != nil {
		return nil, fmt.Errorf("data.GlobalBlockByHash: %w", err)
	}
	// translateGlobalBlock dereferences gb.Header without nil-checking
	// (matches executeBlock's contract on the same type), so reject the
	// unknown-hash case here.
	gb, ok := opt.Get()
	if !ok {
		return &coretypes.ResultBlock{}, nil
	}
	return r.translateGlobalBlock(gb), nil
}

// translateGlobalBlock returns LastCommit with empty Signatures (mirroring
// executeBlock's empty abci.CommitInfo). Under Autobahn the committee is
// fixed by genesis — surfacing N "absent sig" entries here would make
// trace replay's BeginBlock bump missed-block counters and diverge from
// production. ToReqBeginBlock skips the per-validator loop when
// Signatures is empty.
func (r *gigaRouterCommon) translateGlobalBlock(gb *atypes.GlobalBlock) *coretypes.ResultBlock {
	srcTxs := gb.Payload.Txs()
	tmTxs := make(types.Txs, len(srcTxs))
	for i, tx := range srcTxs {
		tmTxs[i] = tx
	}
	h := gb.Header.Hash()
	return &coretypes.ResultBlock{
		BlockID: types.BlockID{Hash: tmbytes.HexBytes(h.Bytes())},
		Block: &types.Block{
			Header: types.Header{
				ChainID: r.cfg.GenDoc.ChainID,
				Height:  utils.Clamp[int64](gb.GlobalNumber),
				Time:    gb.Timestamp,
			},
			Data:       types.Data{Txs: tmTxs},
			LastCommit: &types.Commit{},
		},
	}
}

func (r *gigaRouterCommon) executeBlock(ctx context.Context, b *atypes.GlobalBlock) (*abci.ResponseCommit, error) {
	app := r.app
	hash := b.Header.Hash()
	var proposerAddress types.Address
	if vals := app.GetValidators(); len(vals) > 0 {
		// Deterministically select a proposer from the app's validator committee.
		// We need it so that app does not emit error logs.
		proposer := slices.MinFunc(vals, func(a, b abci.ValidatorUpdate) int { return a.PubKey.Compare(b.PubKey) })
		key, err := crypto.PubKeyFromProto(proposer.PubKey)
		if err != nil {
			return nil, fmt.Errorf("crypto.PubKeyFromProto(): %w", err)
		}
		proposerAddress = key.Address()
	}

	// TODO: add metrics to understand execution latency.
	resp, err := app.FinalizeBlock(ctx, &abci.RequestFinalizeBlock{
		Txs: b.Payload.Txs(),
		// Empty DecidedLastCommit does not indicate missing votes.
		DecidedLastCommit: abci.CommitInfo{},
		// WARNING: this is a hash of the autobahn block header.
		// It is used to identify block processed optimistically
		// and is fed as block hash to EVM contracts.
		Hash: hash[:],
		Header: (&types.Header{
			ChainID: r.cfg.GenDoc.ChainID,
			Height:  int64(b.GlobalNumber), // nolint:gosec // different representations of the same value
			Time:    b.Timestamp,
			// WARNING: the reward distribution has corner cases where it forgets the proposer,
			// because reward is distributed with a delay. This is not our problem here though.
			ProposerAddress: proposerAddress,
		}).ToProto(),
	})
	if err != nil {
		return nil, fmt.Errorf("app.FinalizeBlock(): %w", err)
	}
	commitResp, err := app.Commit(ctx)
	if err != nil {
		return nil, fmt.Errorf("app.Commit(): %w", err)
	}
	// Publish right after Commit (not in the caller loop) so readers of
	// LastCommittedBlockNumber don't briefly see app.LastBlockHeight ahead
	// of our reported height while PushAppHash is still running.
	r.lastExecutedBlock.Store(int64(b.GlobalNumber)) // nolint:gosec // autobahn block numbers fit in int64.
	if err := r.data.PushAppHash(ctx, b.GlobalNumber, resp.AppHash); err != nil {
		return nil, fmt.Errorf("r.data.PushAppHash(%v): %w", b.GlobalNumber, err)
	}
	return commitResp, nil
}

func (r *gigaRouterCommon) runExecute(ctx context.Context) error {
	app := r.app

	info, err := app.Info(ctx, &version.RequestInfo)
	if err != nil {
		return fmt.Errorf("App.Info(): %w", err)
	}
	last, ok := utils.SafeCast[atypes.GlobalBlockNumber](info.LastBlockHeight)
	if !ok {
		return fmt.Errorf("invalid info.LastBlockHeight = %v", info.LastBlockHeight)
	}
	// Seed before the executeBlock loop so LastCommittedBlockNumber is
	// correct on restart (catch-up advances it after each Commit).
	r.lastExecutedBlock.Store(info.LastBlockHeight)
	next := last + 1
	if last == 0 {
		// Fresh start: CometBFT handshaker is skipped in giga mode (see
		// node.go: shouldHandshake = !stateSync && !gigaEnabled), so we
		// call InitChain ourselves. It sets up the app's deliverState
		// against which the first FinalizeBlock below runs.
		//
		// Re-entering on restart (crashed after InitChain, before first
		// Commit) is safe — nothing was committed, so it behaves as a
		// fresh init.
		if _, err := app.InitChain(ctx, r.cfg.GenDoc.ToRequestInitChain()); err != nil {
			return fmt.Errorf("App.InitChain(): %w", err)
		}
		var ok bool
		next, ok = utils.SafeCast[atypes.GlobalBlockNumber](r.cfg.GenDoc.InitialHeight)
		if !ok {
			return fmt.Errorf("invalid GenDoc.InitialHeight = %v", r.cfg.GenDoc.InitialHeight)
		}
	} else {
		// Losing a prefix of appHashes on crash is fine: AppQC is reached
		// once everyone votes on apphashes of a suffix of finalized blocks.
		if err := r.data.PushAppHash(ctx, last, info.LastBlockAppHash); err != nil {
			return fmt.Errorf("r.data.PushAppHash(): %w", err)
		}
	}

	for n := next; ; n += 1 {
		b, err := r.data.GlobalBlock(ctx, n)
		if err != nil {
			return fmt.Errorf("r.data.GlobalBlock(%v): %w", n, err)
		}
		commitResp, err := r.executeBlock(ctx, b)
		if err != nil {
			return fmt.Errorf("r.executeBlock(%v): %w", n, err)
		}
		pruneBefore, ok := utils.SafeCast[atypes.GlobalBlockNumber](commitResp.RetainHeight)
		if !ok {
			return fmt.Errorf("invalid commitResp.RetainHeight = %v", commitResp.RetainHeight)
		}
		if err := r.data.PruneBefore(pruneBefore); err != nil {
			return fmt.Errorf("r.data.PruneBefore(%v): %w", pruneBefore, err)
		}
	}
}

func (r *gigaValidatorRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// Validators dial every committee member in parallel —
		// consensus voting requires fan-out, not stickiness. The same
		// connections also serve block sync between committee peers.
		for _, addr := range r.cfg.ValidatorAddrs {
			s.Spawn(func() error {
				for {
					err := r.dialAndRunConn(ctx, addr.Key, addr.HostPort, r.service.RunClient)
					logger.Info("giga connection failed", "addr", addr, "err", err)
					if err := utils.Sleep(ctx, r.cfg.DialInterval); err != nil {
						return err
					}
				}
			})
		}
		s.SpawnNamed("consensus", func() error { return r.consensus.Run(ctx) })
		s.SpawnNamed("producer", func() error { return r.producer.Run(ctx) })
		r.spawnReadPath(ctx, s)
		return nil
	})
}

// spawnReadPath spawns the three goroutines both router modes run:
// data layer, executeBlock loop, giga block-fetch service. Mode-specific
// spawns live at the call site.
func (r *gigaRouterCommon) spawnReadPath(ctx context.Context, s scope.Scope) {
	s.SpawnNamed("data", func() error { return r.data.Run(ctx) })
	s.SpawnNamed("execute", func() error { return r.runExecute(ctx) })
	s.SpawnNamed("service", func() error { return r.service.Run(ctx) })
}

// dialAndRunConn dials a committee member, handshakes as a SeiGiga
// connection, registers the rpc client in poolOut, and runs runClient
// for the connection's lifetime. runClient is the only mode-specific
// piece (validator: service.RunClient, fullnode: service.RunBlockSyncClient).
func (r *gigaRouterCommon) dialAndRunConn(
	ctx context.Context,
	key NodePublicKey,
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
		// TODO: handshake needs a timeout.
		hConn, err := handshake(ctx, tcpConn, r.key, handshakeSpec{SeiGigaConnection: true})
		if err != nil {
			return fmt.Errorf("handshake(): %w", err)
		}
		if !hConn.msg.SeiGigaConnection {
			return fmt.Errorf("not a sei giga connection")
		}
		if got := hConn.msg.NodeAuth.Key(); got != key {
			return fmt.Errorf("peer key = %v, want %v", got, key)
		}
		client := rpc.NewClient[giga.API]()
		return r.poolOut.InsertAndRun(ctx, key, client, func(ctx context.Context) error {
			return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
				s.Spawn(func() error { return client.Run(ctx, hConn.conn) })
				return runClient(ctx, client)
			})
		})
	})
}

func (r *gigaValidatorRouter) RunInboundConn(ctx context.Context, hConn *handshakedConn) error {
	if !hConn.msg.SeiGigaConnection {
		return fmt.Errorf("not a SeiGiga connection")
	}
	key := hConn.msg.NodeAuth.Key()
	// Committee peers get the full RunServer; non-committee peers get
	// the block-sync subset (StreamFullCommitQCs + GetBlock) capped at
	// inboundFullnodeCap.
	isCommittee := false
	for _, addr := range r.cfg.ValidatorAddrs {
		if addr.Key == key {
			isCommittee = true
			break
		}
	}
	if !isCommittee {
		// Optimistic acquire: Add(1), compare, Add(-1) on overflow.
		if r.inboundFullnodeCount.Add(1) > r.inboundFullnodeCap {
			r.inboundFullnodeCount.Add(-1)
			return fmt.Errorf("inbound fullnode peer limit (%d) reached", r.inboundFullnodeCap)
		}
		defer r.inboundFullnodeCount.Add(-1)
	}
	server := rpc.NewServer[giga.API]()
	return r.poolIn.InsertAndRun(ctx, key, server, func(ctx context.Context) error {
		return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			s.Spawn(func() error { return server.Run(ctx, hConn.conn) })
			if isCommittee {
				return r.service.RunServer(ctx, server)
			}
			return r.service.RunBlockSyncServer(ctx, server)
		})
	})
}

// EvmProxy returns the shard owner's EVMRPC URL for the given sender, or
// (nil, false) when the shard owner is this node itself (validator handles
// its own shard via local mempool). Fullnodes have no validatorKey so the
// self-shard branch is unreachable. The .Get() silent-drop is also
// unreachable in production — validateCommonAndBuildData rejects configs
// where any committee member is missing an EVMRPC URL.
func (r *gigaRouterCommon) EvmProxy(sender common.Address) (*url.URL, bool) {
	shardValidator := r.data.Committee().EvmShard(sender)
	if myKey, ok := r.validatorKey.Get(); ok && myKey == shardValidator {
		return nil, false
	}
	return r.cfg.ValidatorAddrs[shardValidator].EVMRPC.Get()
}

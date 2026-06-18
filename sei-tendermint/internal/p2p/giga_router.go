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
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
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
	EVMRPC   *url.URL
}

func (a GigaNodeAddr) String() string {
	return fmt.Sprintf("%v@%v", a.Key, a.HostPort)
}

// GigaRouterCommonConfig is the slice of giga config shared by both
// validator and fullnode constructors.
type GigaRouterCommonConfig struct {
	DialInterval   time.Duration
	ValidatorAddrs map[atypes.PublicKey]GigaNodeAddr
	GenDoc         *types.GenesisDoc
	// PersistentStateDir is the on-disk root for the data WAL (and the
	// validator's consensus persister in a sibling subdir). None ⇒ in-memory.
	PersistentStateDir utils.Option[string]
	// App is the ABCI proxy executeBlock drives. NewGigaValidatorRouter
	// also copies this into cfg.Producer.App so the producer's internal
	// mempool drives the same proxy.
	App *proxy.Proxy
	// MaxInboundFullnodePeers caps inbound block-sync from non-committee
	// peers. 0 rejects all; positive caps at n.
	MaxInboundFullnodePeers int
}

// GigaValidatorConfig configures a committee-member GigaRouter.
type GigaValidatorConfig struct {
	GigaRouterCommonConfig
	ValidatorKey atypes.SecretKey
	ViewTimeout  func(atypes.View) time.Duration
	// Producer.App is filled by NewGigaValidatorRouter from common.App.
	Producer *producer.Config
}

// GigaRouter is the read-path / Run / EvmProxy surface. Implemented by
// *gigaValidatorRouter and *gigaFullnodeRouter; Mempool returns Some only
// on validators; RunInboundConn errors on fullnodes.
type GigaRouter interface {
	Run(ctx context.Context) error
	RunInboundConn(ctx context.Context, hConn *handshakedConn) error
	LastCommittedBlockNumber() int64
	MaxGasEstimatedPerBlock() uint64
	BlockByNumber(ctx context.Context, n atypes.GlobalBlockNumber) (*coretypes.ResultBlock, error)
	BlockByHash(ctx context.Context, hash atypes.BlockHeaderHash) (*coretypes.ResultBlock, error)
	EvmProxy(sender common.Address) (*url.URL, bool)
	Mempool() utils.Option[*producer.State]
}

type gigaRouterCommon struct {
	cfg     *GigaRouterCommonConfig
	key     NodeSecretKey
	data    *data.State
	service *giga.Service
	poolIn  *giga.Pool[NodePublicKey, rpc.Server[giga.API]]
	poolOut *giga.Pool[NodePublicKey, rpc.Client[giga.API]]
	app     *proxy.Proxy

	// inboundFullnodeCount tracks live non-committee inbound block-sync
	// connections. Optimistic Add(1) + compare against cap; over-rejects
	// by one or two under contention but never over-accepts.
	inboundFullnodeCount atomic.Int32
	inboundFullnodeCap   int32
}

type gigaValidatorRouter struct {
	*gigaRouterCommon

	consensus      *consensus.State
	producer       *producer.State
	producerConfig *producer.Config
	// validatorKey is the cached public form of cfg.ValidatorKey, used by
	// EvmProxy to short-circuit self-shard sends to the local mempool.
	validatorKey atypes.PublicKey
}

// buildDataState validates the common config and constructs the data
// layer (committee, WAL, State) shared by both giga constructors.
//
// TODO(autobahn): once sei-db/ledger_db/block.BlockDB has a writer wired
// (see BlockByNumber's TODO), the data WAL is redundant.
func buildDataState(cfg *GigaRouterCommonConfig) (*data.State, error) {
	if cfg.GenDoc.InitialHeight < 1 {
		return nil, fmt.Errorf("GenDoc.InitialHeight = %v, want >=1", cfg.GenDoc.InitialHeight)
	}
	if cfg.DialInterval <= 0 {
		return nil, fmt.Errorf("GigaRouterCommonConfig.DialInterval = %v, want > 0", cfg.DialInterval)
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

func NewGigaFullnodeRouter(cfg *GigaRouterCommonConfig, key NodeSecretKey) (*gigaFullnodeRouter, error) {
	dataState, err := buildDataState(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.MaxInboundFullnodePeers < 0 {
		return nil, fmt.Errorf("GigaRouterCommonConfig.MaxInboundFullnodePeers = %v, want >= 0", cfg.MaxInboundFullnodePeers)
	}
	logger.Info("GigaRouter initialized (fullnode)", "validators", len(cfg.ValidatorAddrs), "inbound_fullnode_cap", cfg.MaxInboundFullnodePeers)
	return &gigaFullnodeRouter{
		gigaRouterCommon: &gigaRouterCommon{
			cfg:                cfg,
			key:                key,
			data:               dataState,
			service:            giga.NewBlockSyncService(dataState),
			poolIn:             giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
			poolOut:            giga.NewPool[NodePublicKey, rpc.Client[giga.API]](),
			app:                cfg.App,
			inboundFullnodeCap: int32(cfg.MaxInboundFullnodePeers), // nolint:gosec // validated >= 0 above.
		},
	}, nil
}

func NewGigaValidatorRouter(cfg *GigaValidatorConfig, key NodeSecretKey) (*gigaValidatorRouter, error) {
	dataState, err := buildDataState(&cfg.GigaRouterCommonConfig)
	if err != nil {
		return nil, err
	}
	// One App per node — common owns it; mirror into producer.Config so
	// the producer's internal mempool drives the same ABCI proxy.
	//
	// TODO(autobahn): drop App from producer.Config and pass it to
	// producer.NewState as a constructor arg — App is a runtime dependency,
	// not configuration, and common is the canonical home now that
	// fullnodes also need it.
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
		return nil, fmt.Errorf("GigaRouterCommonConfig.MaxInboundFullnodePeers = %v, want >= 0", cfg.MaxInboundFullnodePeers)
	}
	logger.Info("GigaRouter initialized", "validators", len(cfg.ValidatorAddrs), "dial_interval", cfg.DialInterval, "inbound_fullnode_cap", cfg.MaxInboundFullnodePeers)
	return &gigaValidatorRouter{
		gigaRouterCommon: &gigaRouterCommon{
			cfg:                &cfg.GigaRouterCommonConfig,
			key:                key,
			data:               dataState,
			service:            giga.NewService(consensusState),
			poolIn:             giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
			poolOut:            giga.NewPool[NodePublicKey, rpc.Client[giga.API]](),
			app:                cfg.App,
			inboundFullnodeCap: int32(cfg.MaxInboundFullnodePeers), // nolint:gosec // validated >= 0 above.
		},
		consensus:      consensusState,
		producer:       producerState,
		producerConfig: cfg.Producer,
		validatorKey:   cfg.ValidatorKey.Public(),
	}, nil
}

func (r *gigaRouterCommon) LastCommittedBlockNumber() int64 {
	return r.app.LastBlockHeight()
}

func (r *gigaValidatorRouter) MaxGasEstimatedPerBlock() uint64 {
	return r.producerConfig.MaxGasEstimatedPerBlock
}

func (r *gigaValidatorRouter) Mempool() utils.Option[*producer.State] {
	return utils.Some(r.producer)
}

// BlockByNumber returns the finalized global block at height n translated
// into the CometBFT coretypes.ResultBlock shape. This lets consumers
// (notably evmrpc, which wraps receipts/logs with block context) keep
// working under Autobahn without CometBFT's BlockStore being populated.
//
// Fields populated when the underlying GlobalBlock is well-formed:
// BlockID.Hash (Autobahn lane-block header hash — the same bytes passed to
// app.FinalizeBlock's Hash param, which the EVM receipt store records as
// blockHash), Block.Header.ChainID/Height/Time, Block.Data.Txs. Other
// fields (AppHash, ProposerAddress, LastCommit, …) stay at zero values —
// evmrpc does not read them on the receipt path. If gb.Header is nil
// BlockID.Hash also stays empty; if gb.Payload is nil Block.Data.Txs
// stays empty (see the malformed-block handling below).
//
// TODO(autobahn): switch this to read from sei-db/ledger_db/block.BlockDB
// once a writer is wired (e.g. from app.FinalizeBlocker or executeBlock).
// Today no production code calls BlockDB.WriteBlock, so Autobahn's in-memory
// data.State is the only place a full block lives — but it's pruned per
// Sei's RetainHeight and exposes only a height index (no GetBlockByHash).
// BlockDB has the right shape (height + hash indexes, async pruning) and
// is the long-term home for this read path.
func (r *gigaRouterCommon) BlockByNumber(ctx context.Context, n atypes.GlobalBlockNumber) (*coretypes.ResultBlock, error) {
	gb, err := r.data.GlobalBlock(ctx, n)
	if err != nil {
		// Map Autobahn's pruning sentinel to CometBFT's, so callers
		// (env.Block, evmrpc, ops tooling) get the same error type they
		// already handle on the CometBFT path. base is None because the
		// active lower bound (data.State.inner.first) is internal to
		// data.State; both call sites format through the same helper.
		if errors.Is(err, data.ErrPruned) {
			return nil, coretypes.WrapErrHeightNotAvailable(utils.Clamp[int64](n), utils.None[int64]())
		}
		return nil, fmt.Errorf("data.GlobalBlock(%v): %w", n, err)
	}
	return r.translateGlobalBlock(gb), nil
}

// BlockByHash returns the finalized global block keyed by Autobahn block-
// header hash, translated into the CometBFT coretypes.ResultBlock shape
// (same translation as BlockByNumber). Matches CometBFT semantics for
// unknown hashes: returns &ResultBlock{Block: nil} with no error.
//
// Lookup-and-construct happens under a single data.State lock acquire, so
// the returned block matches the requested hash atomically. Hashes below
// the pruning watermark are not indexed and read as "unknown". Wrong-size
// inputs are rejected at the call site (env.BlockByHash) so this method
// can stay strongly typed on atypes.BlockHeaderHash.
//
// TODO(autobahn): replace this with a direct read from
// sei-db/ledger_db/block.BlockDB.GetBlockByHash once a writer is wired into
// block execution. The data.State-side index can also go away at that point.
func (r *gigaRouterCommon) BlockByHash(ctx context.Context, hash atypes.BlockHeaderHash) (*coretypes.ResultBlock, error) {
	opt, err := r.data.GlobalBlockByHash(hash)
	if err != nil {
		return nil, fmt.Errorf("data.GlobalBlockByHash: %w", err)
	}
	// Reject the unknown-hash case here so translateGlobalBlock can rely
	// on the *GlobalBlock type contract (non-nil, with non-nil Header
	// and Payload) — same way executeBlock dereferences b.Header
	// without checking. Mirrors CometBFT's BlockStore.LoadBlockByHash
	// returning &ResultBlock{Block: nil} for an unknown hash.
	gb, ok := opt.Get()
	if !ok {
		return &coretypes.ResultBlock{}, nil
	}
	return r.translateGlobalBlock(gb), nil
}

// translateGlobalBlock converts an Autobahn GlobalBlock to the CometBFT
// coretypes.ResultBlock shape used by env.Block / env.BlockByHash and
// downstream evmrpc consumers. Caller must pass a non-nil *GlobalBlock with
// non-nil Header and Payload — that's the contract data.State guarantees on
// a successful lookup, and matches how executeBlock dereferences b.Header
// without a nil-check on the same type. The "no such block" case is
// rejected at the BlockByHash call site before delegating here.
//
// LastCommit is non-nil with empty Signatures, mirroring executeBlock's
// FinalizeBlock call which passes an empty abci.CommitInfo. Under Autobahn
// the committee is fixed by genesis (no validator-set updates), so the
// application is not in control of jailing — surfacing N "absent sig"
// entries here would make trace replay's BeginBlock bump missed-block
// counters and diverge from production. ToReqBeginBlock skips the per-
// validator loop when Signatures is empty, so empty Votes flow into
// distribution/slashing on both paths.
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
				// Clamp accepts any constraints.Integer for From, so
				// gb.GlobalNumber (a typed uint64) goes in directly — no
				// intermediate uint64() conversion needed.
				Height: utils.Clamp[int64](gb.GlobalNumber),
				Time:   gb.Timestamp,
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

// dialAndRunConn dials a peer, handshakes as a SeiGiga connection,
// registers the rpc client in poolOut, and runs runClient for the
// connection's lifetime. expectedKey is enforced when Some (validator
// dialing a committee member); fullnodes pass None — block-sync data
// is QC-verified, so the peer's identity doesn't need to be checked
// here.
func (r *gigaRouterCommon) dialAndRunConn(
	ctx context.Context,
	expectedKey utils.Option[NodePublicKey],
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
		peerKey := hConn.msg.NodeAuth.Key()
		if want, ok := expectedKey.Get(); ok && peerKey != want {
			return fmt.Errorf("peer key = %v, want %v", peerKey, want)
		}
		client := rpc.NewClient[giga.API]()
		return r.poolOut.InsertAndRun(ctx, peerKey, client, func(ctx context.Context) error {
			return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
				s.Spawn(func() error { return client.Run(ctx, hConn.conn) })
				return runClient(ctx, client)
			})
		})
	})
}

// RunInboundConn serves an inbound giga connection. Non-committee peers
// get the block-sync subset (StreamFullCommitQCs + GetBlock), capped at
// inboundFullnodeCap. Committee peers get the full RunServer on
// validators; fullnodes don't run consensus/avail so committee peers
// (rare — validators normally dial each other, not us) also get block-sync.
func (r *gigaRouterCommon) RunInboundConn(ctx context.Context, hConn *handshakedConn) error {
	if !hConn.msg.SeiGigaConnection {
		return fmt.Errorf("not a SeiGiga connection")
	}
	// Filter unwanded connections.
	key := hConn.msg.NodeAuth.Key()
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
			if isCommittee && r.service.HasConsensusState() {
				return r.service.RunServer(ctx, server)
			}
			return r.service.RunBlockSyncServer(ctx, server)
		})
	})
}

// EvmProxy returns the shard owner's EVMRPC URL for an EVM tx sender.
// Overridden on *gigaValidatorRouter to short-circuit self-shard sends.
func (r *gigaRouterCommon) EvmProxy(sender common.Address) (*url.URL, bool) {
	shardValidator := r.data.Committee().EvmShard(sender)
	return r.cfg.ValidatorAddrs[shardValidator].EVMRPC, true
}

// EvmProxy on the validator returns (nil, false) when the sender's shard
// owner is us (handle locally via mempool, no HTTP round-trip to self).
func (r *gigaValidatorRouter) EvmProxy(sender common.Address) (*url.URL, bool) {
	shardValidator := r.data.Committee().EvmShard(sender)
	if r.validatorKey == shardValidator {
		return nil, false
	}
	return r.cfg.ValidatorAddrs[shardValidator].EVMRPC, true
}

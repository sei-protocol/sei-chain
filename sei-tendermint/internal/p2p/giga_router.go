package p2p

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/common"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
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

type GigaRouterConfig struct {
	DialInterval   time.Duration
	ValidatorAddrs map[atypes.PublicKey]GigaNodeAddr
	Consensus      *consensus.Config
	// Producer is only set on validator paths; rpc-only nodes don't
	// produce blocks and source MaxGasPerBlock from genesis instead.
	Producer  utils.Option[*producer.Config]
	TxMempool *mempool.TxMempool
	GenDoc    *types.GenesisDoc
	// RPCOnly selects the rpc-only construction path in NewGigaRouter.
	// Rpc-only nodes build data.State + the block-sync subscriber (no
	// consensus, producer, or full giga service inbound) and forward
	// eth_sendRawTransaction over EvmProxy. Producer is unused;
	// Consensus contributes only PersistentStateDir for the data WAL.
	RPCOnly bool
	// MaxInboundRPCOnlyPeers overrides defaultMaxInboundRPCOnlyPeers when
	// > 0. Validator-only; ignored on rpc-only nodes.
	MaxInboundRPCOnlyPeers int
}

// GigaRouter is the per-node entry point into the Autobahn stack. Two
// concrete implementations live behind this interface: gigaValidatorRouter
// (committee members running consensus + producer + inbound giga service)
// and gigaRPCOnlyRouter (non-validators that pull finalized blocks from
// committee members and execute them locally). NewGigaRouter dispatches
// on GigaRouterConfig.RPCOnly and returns the appropriate impl. External
// callers (router.go, internal/rpc/core/{blocks,status,mempool}.go) reach
// in through this interface.
type GigaRouter interface {
	Run(ctx context.Context) error
	RunInboundConn(ctx context.Context, hConn *handshakedConn) error
	LastCommittedBlockNumber() int64
	MaxGasPerBlock() int64
	BlockByNumber(ctx context.Context, n atypes.GlobalBlockNumber) (*coretypes.ResultBlock, error)
	BlockByHash(ctx context.Context, hash atypes.BlockHeaderHash) (*coretypes.ResultBlock, error)
	EvmProxy(sender common.Address) (*url.URL, bool)
	IsRPCOnly() bool
}

// defaultMaxInboundRPCOnlyPeers caps concurrent inbound block-sync
// connections from non-committee peers per validator when
// GigaRouterConfig.MaxInboundRPCOnlyPeers is not set. Without admission
// control any peer could open unbounded streams; this flat cap is a
// simple first defence.
//
// TODO(autobahn-trusted-rpc-peers): add an optional trusted-peer list
// whose keys bypass the cap.
const defaultMaxInboundRPCOnlyPeers = 10

// gigaRouterCommon holds the GigaRouter fields and methods that are
// bit-identical between validator and rpc-only modes. Both concrete impls
// embed *gigaRouterCommon and inherit the read-path / execute-loop logic
// from it; mode-specific behaviour (Run, RunInboundConn, EvmProxy,
// LastCommittedBlockNumber, MaxGasPerBlock, dialAndRunConn) is implemented
// on the embedding types.
type gigaRouterCommon struct {
	cfg     *GigaRouterConfig
	key     NodeSecretKey
	data    *data.State
	service *giga.Service
	poolOut *giga.Pool[NodePublicKey, rpc.Client[giga.API]]
}

// gigaValidatorRouter is the GigaRouter impl for committee members. It
// runs consensus + producer, accepts inbound giga connections (full
// service for committee peers, block-sync subset for rpc-only peers), and
// dials every committee member in parallel for vote fan-out.
type gigaValidatorRouter struct {
	*gigaRouterCommon

	consensus      *consensus.State
	producer       *producer.State
	producerConfig *producer.Config
	poolIn         *giga.Pool[NodePublicKey, rpc.Server[giga.API]]

	// inboundRPCOnlyPermits is a buffered-channel semaphore capping
	// concurrent non-committee inbound block-sync connections.
	// Non-blocking acquire (select + default) so excess peers get a clean
	// rejection at handshake time rather than queuing.
	inboundRPCOnlyPermits chan struct{}
	// inboundRPCOnlyCap is the configured cap, captured in the panic
	// message when rejection fires.
	inboundRPCOnlyCap int

	// lastCommitQCRecv is subscribed once at construction and reused for the
	// lifetime of the GigaRouter. Load() is lock-free (a single
	// atomic.Pointer.Load).
	//
	// Staleness-safety: the receiver points at the same atomicWatch held inside
	// avail.inner.latestCommitQC — a value field on a heap-allocated *inner
	// that is never replaced for the lifetime of the State, only Store()d
	// into. Every Load therefore observes the most recent Store. A
	// reconstructed avail.State (only on process restart) would also
	// reconstruct this GigaRouter, so the receiver can't outlive its watch.
	lastCommitQCRecv utils.AtomicRecv[utils.Option[*atypes.CommitQC]]
}

func NewGigaRouter(cfg *GigaRouterConfig, key NodeSecretKey) (GigaRouter, error) {
	if cfg.GenDoc.InitialHeight < 1 {
		return nil, fmt.Errorf("GenDoc.InitialHeight = %v, want >=1", cfg.GenDoc.InitialHeight)
	}
	// DialInterval feeds the outbound dial-retry sleep on both paths and
	// the backoff base on the rpc-only path; <= 0 would spin both loops.
	// AutobahnFileConfig.Validate already rejects this when loading from
	// disk; this guard catches direct GigaRouterConfig construction.
	if cfg.DialInterval <= 0 {
		return nil, fmt.Errorf("GigaRouterConfig.DialInterval = %v, want > 0", cfg.DialInterval)
	}
	committee, err := atypes.NewRoundRobinElection(
		slices.Collect(maps.Keys(cfg.ValidatorAddrs)),
		atypes.GlobalBlockNumber(cfg.GenDoc.InitialHeight), // nolint:gosec // verified to be positive.
		cfg.GenDoc.GenesisTime,
	)
	if err != nil {
		return nil, fmt.Errorf("atypes.NewRoundRobinElection(): %w", err)
	}
	// Automated pruning is disabled, because it is controlled by the application.
	// The data WAL piggybacks on Consensus.PersistentStateDir: the two layers
	// share the same on-disk root and write to distinct subdirectories under
	// it (inner / blocks / commitqcs for consensus, globalblocks /
	// fullcommitqcs for data). Rpc-only nodes use the same PersistentStateDir
	// for the data layer alone (no consensus subdirs).
	//
	// TODO(autobahn): once sei-db/ledger_db/block.BlockDB has a writer wired
	// (see BlockByNumber's TODO), the data layer's WAL is redundant —
	// BlockDB is the long-term home for the block read path and survives
	// process restarts on its own. At that point this NewDataWAL call can
	// drop the directory and become a no-op.
	dataWAL, err := data.NewDataWAL(cfg.Consensus.PersistentStateDir, committee)
	if err != nil {
		return nil, fmt.Errorf("data.NewDataWAL(): %w", err)
	}
	dataState, err := data.NewState(&data.Config{Committee: committee}, dataWAL)
	if err != nil {
		return nil, fmt.Errorf("data.NewState(): %w", err)
	}
	if cfg.RPCOnly {
		// Every committee member must expose an EVMRPC URL: rpc-only nodes
		// can't produce blocks locally, so EvmProxy is the only path a
		// submitted EVM tx can take. A missing URL on the shard owner
		// would otherwise let EvmProxy return (nil, false), which the
		// evmrpc send path interprets as "handle locally" — silently
		// mempooling a tx that will never land in a block. Catch the
		// misconfiguration at startup.
		for vk, addr := range cfg.ValidatorAddrs {
			if !addr.EVMRPC.IsPresent() {
				return nil, fmt.Errorf("rpc-only: validator %s is missing evmrpc URL; every committee member must expose one so EvmProxy can forward", vk)
			}
		}
		logger.Info("GigaRouter initialized (rpc-only)", "validators", len(cfg.ValidatorAddrs))
		return &gigaRPCOnlyRouter{
			gigaRouterCommon: &gigaRouterCommon{
				cfg:     cfg,
				key:     key,
				data:    dataState,
				service: giga.NewBlockSyncService(dataState),
				poolOut: giga.NewPool[NodePublicKey, rpc.Client[giga.API]](),
			},
		}, nil
	}
	consensusState, err := consensus.NewState(cfg.Consensus, dataState)
	if err != nil {
		return nil, fmt.Errorf("consensus.NewState(): %w", err)
	}
	producerConfig := cfg.Producer.OrPanic("validator-mode requires GigaRouterConfig.Producer")
	producerState := producer.NewState(producerConfig, cfg.TxMempool, consensusState)
	inboundRPCOnlyCap := cfg.MaxInboundRPCOnlyPeers
	if inboundRPCOnlyCap <= 0 {
		inboundRPCOnlyCap = defaultMaxInboundRPCOnlyPeers
	}
	logger.Info("GigaRouter initialized", "validators", len(cfg.ValidatorAddrs), "dial_interval", cfg.DialInterval, "inbound_rpc_only_cap", inboundRPCOnlyCap)
	return &gigaValidatorRouter{
		gigaRouterCommon: &gigaRouterCommon{
			cfg:     cfg,
			key:     key,
			data:    dataState,
			service: giga.NewService(consensusState),
			poolOut: giga.NewPool[NodePublicKey, rpc.Client[giga.API]](),
		},
		consensus:             consensusState,
		producer:              producerState,
		producerConfig:        producerConfig,
		poolIn:                giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
		inboundRPCOnlyPermits: make(chan struct{}, inboundRPCOnlyCap),
		inboundRPCOnlyCap:     inboundRPCOnlyCap,
		// Subscribe once here (takes avail's internal lock once);
		// subsequent Load() calls from RPC handlers are lock-free atomic
		// pointer reads.
		lastCommitQCRecv: consensusState.Avail().LastCommitQC(),
	}, nil
}

// IsRPCOnly reports whether this router was constructed in rpc-only
// (non-validator) mode. Rpc-only nodes pull finalized blocks from
// committee members and execute them locally; they don't produce blocks
// or participate in consensus voting.
func (r *gigaValidatorRouter) IsRPCOnly() bool { return false }

// LastCommittedBlockNumber returns the highest global block number finalized
// by consensus. Validators read from avail.State's LastCommitQC watch.
// Safe for high-frequency callers — the path is lock-free.
func (r *gigaValidatorRouter) LastCommittedBlockNumber() int64 {
	// GlobalRange is a half-open [First, Next) interval; the highest
	// committed block number is Next-1.
	gr := atypes.GlobalRangeOpt(r.lastCommitQCRecv.Load(), r.data.Committee())
	return int64(gr.Next) - 1 // nolint:gosec // gr.Next is uint64 but bounded by actual chain height.
}

// MaxGasPerBlock returns the producer's configured max gas per block (int64).
// Thin pass-through to producer.Config.MaxGasPerBlockI64 — the clamp logic
// lives there. Exposed at the GigaRouter level so the RPC layer can populate
// ResultBlockResults.ConsensusParamUpdates under Autobahn (where
// FinalizeBlock responses are not stored on disk) without reaching into
// the unexported router.cfg. Reads producerConfig cached at construction,
// so this stays lock-free and Option-unwrap-free on the hot RPC path.
func (r *gigaValidatorRouter) MaxGasPerBlock() int64 {
	return r.producerConfig.MaxGasPerBlockI64()
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
	app := r.cfg.TxMempool.App()
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
	r.cfg.TxMempool.Lock()
	defer r.cfg.TxMempool.Unlock()

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
		return nil, fmt.Errorf("r.cfg.App.FinalizeBlock(): %w", err)
	}
	if err := r.data.PushAppHash(ctx, b.GlobalNumber, resp.AppHash); err != nil {
		return nil, fmt.Errorf("r.data.PushAppHash(%v): %w", b.GlobalNumber, err)
	}
	commitResp, err := app.Commit(ctx)
	if err != nil {
		return nil, fmt.Errorf("r.cfg.App.Commit(): %w", err)
	}
	blockTxs := make(types.Txs, len(b.Payload.Txs()))
	for i, tx := range b.Payload.Txs() {
		blockTxs[i] = tx
	}
	err = r.cfg.TxMempool.Update(
		ctx,
		int64(b.GlobalNumber), // nolint:gosec // autobahn block numbers fit in int64.
		blockTxs,
		resp.TxResults,
		// TODO: We need the constraints to be fixed per epoch, because we don't know where the lane blocks will be sequenced.
		// Therefore we disable constraints for now, until epochs are supported AND
		// chain state understands that consensus parameters can change only at the epoch boundary.
		mempool.NopTxConstraints(),
		// recheck=false; see TxMempool.Update doc for why.
		false,
	)
	if err != nil {
		return nil, fmt.Errorf("r.cfg.TxMempool.Update(%v): %w", b.GlobalNumber, err)
	}
	return commitResp, nil
}

func (r *gigaRouterCommon) runExecute(ctx context.Context) error {
	app := r.cfg.TxMempool.App()

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
		// Fresh start: the CometBFT handshaker is skipped in giga mode
		// (see node.go: shouldHandshake = !stateSync && !gigaEnabled), so
		// nobody has called InitChain yet. Call it here ourselves; this sets
		// up the app's deliverState (matching real SDK: InitChain leaves
		// deliverState populated with no intermediate Commit, so the first
		// FinalizeBlock below runs against it).
		//
		// On restart (last > 0, below), InitChain must NOT be called again;
		// the app's committed CMS already holds the latest state, and
		// BaseApp.FinalizeBlock rebuilds deliverState from it via its
		// nil-check fallback.
		//
		// Note: if a process crashed after InitChain but before the first
		// Commit, LastBlockHeight is still 0 and we enter this branch again
		// on restart. Re-calling InitChain is safe in that case because
		// nothing was committed — it behaves as a fresh init.
		if _, err := app.InitChain(ctx, r.cfg.GenDoc.ToRequestInitChain()); err != nil {
			return fmt.Errorf("App.InitChain(): %w", err)
		}
		var ok bool
		next, ok = utils.SafeCast[atypes.GlobalBlockNumber](r.cfg.GenDoc.InitialHeight)
		if !ok {
			return fmt.Errorf("invalid GenDoc.InitialHeight = %v", r.cfg.GenDoc.InitialHeight)
		}
	} else {
		// NOTE that with the current implementation losing prefix of appHashes on crash is fine:
		// if everyone votes on apphashes of a suffix of finalized blocks, then AppQC will be reached.
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

// spawnReadPath spawns the three goroutines that both validator and
// rpc-only routers run: the data layer, the executeBlock loop, and the
// giga service (block fetcher). Mode-specific spawns (dial loop +
// consensus/producer on the validator path; the single-active subscriber
// on the rpc-only path) live at each Run call site.
func (r *gigaRouterCommon) spawnReadPath(ctx context.Context, s scope.Scope) {
	s.SpawnNamed("data", func() error { return r.data.Run(ctx) })
	s.SpawnNamed("execute", func() error { return r.runExecute(ctx) })
	s.SpawnNamed("service", func() error { return r.service.Run(ctx) })
}

// dialAndRunConn dials a committee member, handshakes as a SeiGiga
// connection, registers the resulting rpc client in poolOut, and hands
// off to `runClient` for the per-connection lifetime. The runClient
// callback is the only mode-specific piece (validator uses the full
// service.RunClient; rpc-only uses service.RunBlockSyncClient), so the
// dial/handshake plumbing lives once on gigaRouterCommon.
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
	// Committee members get the full RunServer; every other inbound peer
	// is treated as rpc-only and gets the block-sync subset
	// (StreamFullCommitQCs + GetBlock), capped at r.inboundRPCOnlyCap
	// concurrent connections per validator.
	isCommittee := false
	for _, addr := range r.cfg.ValidatorAddrs {
		if addr.Key == key {
			isCommittee = true
			break
		}
	}
	if !isCommittee {
		select {
		case r.inboundRPCOnlyPermits <- struct{}{}:
			defer func() { <-r.inboundRPCOnlyPermits }()
		default:
			return fmt.Errorf("inbound rpc-only peer limit (%d) reached", r.inboundRPCOnlyCap)
		}
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

func (r *gigaValidatorRouter) EvmProxy(sender common.Address) (*url.URL, bool) {
	shardValidator := r.data.Committee().EvmShard(sender)
	// Validators short-circuit when they own the shard so the tx is
	// checked into the local mempool instead.
	if r.cfg.Consensus.Key.Public() == shardValidator {
		return nil, false
	}
	return r.cfg.ValidatorAddrs[shardValidator].EVMRPC.Get()
}

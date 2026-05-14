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
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
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
	Producer       *producer.Config
	TxMempool      *mempool.TxMempool
	GenDoc         *types.GenesisDoc
}

type GigaRouter struct {
	cfg       *GigaRouterConfig
	key       NodeSecretKey
	data      *data.State
	producer  *producer.State
	consensus *consensus.State
	service   *giga.Service
	poolIn    *giga.Pool[NodePublicKey, rpc.Server[giga.API]]
	poolOut   *giga.Pool[NodePublicKey, rpc.Client[giga.API]]

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

func NewGigaRouter(cfg *GigaRouterConfig, key NodeSecretKey) (*GigaRouter, error) {
	if cfg.GenDoc.InitialHeight < 1 {
		return nil, fmt.Errorf("GenDoc.InitialHeight = %v, want >=1", cfg.GenDoc.InitialHeight)
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
	dataWAL, err := data.NewDataWAL(utils.None[string](), committee)
	if err != nil {
		return nil, fmt.Errorf("data.NewDataWAL(): %w", err)
	}
	dataState, err := data.NewState(&data.Config{Committee: committee}, dataWAL)
	if err != nil {
		return nil, fmt.Errorf("data.NewState(): %w", err)
	}
	consensusState, err := consensus.NewState(cfg.Consensus, dataState)
	if err != nil {
		return nil, fmt.Errorf("consensus.NewState(): %w", err)
	}
	producerState := producer.NewState(cfg.Producer, cfg.TxMempool, consensusState)
	logger.Info("GigaRouter initialized", "validators", len(cfg.ValidatorAddrs), "dial_interval", cfg.DialInterval)
	return &GigaRouter{
		cfg:       cfg,
		key:       key,
		data:      dataState,
		consensus: consensusState,
		producer:  producerState,
		service:   giga.NewService(consensusState),
		poolIn:    giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
		poolOut:   giga.NewPool[NodePublicKey, rpc.Client[giga.API]](),

		// Subscribe once here (takes avail's internal lock once); subsequent
		// Load() calls from RPC handlers are lock-free atomic pointer reads.
		lastCommitQCRecv: consensusState.Avail().LastCommitQC(),
	}, nil
}

// LastCommittedBlockNumber returns the highest global block number finalized
// by consensus (derived from the latest CommitQC). When no CommitQC has been
// recorded yet, atypes.GlobalRangeOpt returns the committee's empty default
// range {First: FirstBlock, Next: FirstBlock}, so this returns FirstBlock-1.
// Safe for high-frequency callers — uses a cached lock-free receiver; no
// locks taken on this path.
func (r *GigaRouter) LastCommittedBlockNumber() int64 {
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
// the unexported router.cfg.
func (r *GigaRouter) MaxGasPerBlock() int64 {
	return r.cfg.Producer.MaxGasPerBlockI64()
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
func (r *GigaRouter) BlockByNumber(ctx context.Context, n atypes.GlobalBlockNumber) (*coretypes.ResultBlock, error) {
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
func (r *GigaRouter) BlockByHash(ctx context.Context, hash atypes.BlockHeaderHash) (*coretypes.ResultBlock, error) {
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
func (r *GigaRouter) translateGlobalBlock(gb *atypes.GlobalBlock) *coretypes.ResultBlock {
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

func (r *GigaRouter) executeBlock(ctx context.Context, b *atypes.GlobalBlock) (*abci.ResponseCommit, error) {
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
		mempool.NopTxConstraintsFetcher,
		// recheck=false; see TxMempool.Update doc for why.
		false,
	)
	if err != nil {
		return nil, fmt.Errorf("r.cfg.TxMempool.Update(%v): %w", b.GlobalNumber, err)
	}
	return commitResp, nil
}

func (r *GigaRouter) runExecute(ctx context.Context) error {
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

func (r *GigaRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// Spawn outbound connections dialing.
		for _, addr := range r.cfg.ValidatorAddrs {
			s.Spawn(func() error {
				for {
					err := r.dialAndRunConn(ctx, addr.Key, addr.HostPort)
					logger.Info("giga connection failed", "addr", addr, "err", err)
					if err := utils.Sleep(ctx, r.cfg.DialInterval); err != nil {
						return err
					}
				}
			})
		}
		s.SpawnNamed("data", func() error { return r.data.Run(ctx) })
		s.SpawnNamed("consensus", func() error { return r.consensus.Run(ctx) })
		s.SpawnNamed("producer", func() error { return r.producer.Run(ctx) })
		s.SpawnNamed("execute", func() error { return r.runExecute(ctx) })
		s.SpawnNamed("service", func() error { return r.service.Run(ctx) })
		return nil
	})
}

func (r *GigaRouter) dialAndRunConn(ctx context.Context, key NodePublicKey, hp tcp.HostPort) error {
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
				return r.service.RunClient(ctx, client)
			})
		})
	})
}

func (r *GigaRouter) RunInboundConn(ctx context.Context, hConn *handshakedConn) error {
	if !hConn.msg.SeiGigaConnection {
		return fmt.Errorf("not a SeiGiga connection")
	}
	// Filter unwanded connections.
	key := hConn.msg.NodeAuth.Key()
	ok := false
	for _, addr := range r.cfg.ValidatorAddrs {
		if addr.Key == key {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("peer not whitelisted")
	}
	server := rpc.NewServer[giga.API]()
	return r.poolIn.InsertAndRun(ctx, key, server, func(ctx context.Context) error {
		return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			s.Spawn(func() error { return server.Run(ctx, hConn.conn) })
			return r.service.RunServer(ctx, server)
		})
	})
}

func (r *GigaRouter) EvmProxyTx(ctx context.Context, sender common.Address, txRaw []byte) (bool, error) {
	shardValidator := r.data.Committee().EvmShard(sender)
	if r.cfg.Consensus.Key.Public() == shardValidator {
		return false, nil
	}
	url, ok := r.cfg.ValidatorAddrs[shardValidator].EVMRPC.Get()
	if !ok {
		return false, nil
	}
	client, err := ethrpc.DialContext(ctx, url.String())
	if err != nil {
		return true, fmt.Errorf("rpc.DialContext(%q): %w", url.String(), err)
	}
	defer client.Close()

	var hash common.Hash
	if err := client.CallContext(ctx, &hash, "eth_sendRawTransaction", hexutil.Bytes(txRaw)); err != nil {
		return true, fmt.Errorf("CallContext(eth_sendRawTransaction, %q): %w", url.String(), err)
	}
	return true, nil
}

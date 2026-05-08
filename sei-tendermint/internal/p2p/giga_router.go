package p2p

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
	memblockdb "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/mem_block_db"
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
	// blockDB indexes finalized blocks by hash and tracks per-tx execution
	// results. Populated by runExecute: WriteBlock lands just before each
	// block is handed to executeBlock; SetTransactionResults follows once
	// FinalizeBlock returns. Read by BlockByHash and Tx.
	//
	// Today's instance is mem_block_db (in-memory), so it does not survive
	// process restarts — RPC semantics treat that as "unknown hash"
	// (BlockByHash returns &ResultBlock{Block: nil}; Tx returns
	// "tx not found").
	//
	// TODO(autobahn): make BlockDB injectable via GigaRouterConfig (today
	// it's hard-coded to mem_block_db.NewMemBlockDB() in NewGigaRouter,
	// and unit tests reach into this unexported field). Will land
	// alongside the persistent backend follow-up.
	//
	// TODO(autobahn): wire blockDB.Prune from runExecute. Today only
	// data.PruneBefore runs; mem_block_db grows without bound across the
	// chain's lifetime and a long-running process will OOM.
	blockDB block.BlockDB

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
		blockDB:   memblockdb.NewMemBlockDB(),

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

// ErrTxResultPending is returned by Tx when a transaction is known
// (its parent block has been written to BlockDB) but no execution
// result has been attached yet — the window between WriteBlock and
// SetTransactionResults inside runExecute. Distinct from "not found"
// because the tx is real.
//
// On the happy path the caller can retry and the result will land in
// milliseconds. On the unhappy path (executeBlock errored, runExecute
// exited, process is shutting down) the result will never land and
// retry never succeeds — operators inspecting a dead node via RPC will
// see this sentinel forever for any tx in the orphaned block.
//
// Callers that don't care about the distinction can errors.Is-check
// to fold it into a generic "try again" flow.
var ErrTxResultPending = errors.New("transaction result not yet recorded")

// Tx returns the finalized transaction with the given hash translated into
// the CometBFT coretypes.ResultTx shape. Mirrors BlockByHash: the RPC layer
// (env.Tx) just delegates here when Autobahn is active, keeping the
// abci.ExecTxResult unmarshal and ResultTx assembly inside the giga
// package. Match CometBFT semantics for unknown hashes — return an error
// rather than nil — since callers (broadcast_tx_commit polling, ops
// tooling) already handle that error explicitly.
//
// req.Prove is intentionally not honored — Autobahn doesn't materialize
// types.TxProof, and tooling that needs it falls back to the CometBFT path.
//
// When the same tx hash was included in multiple blocks (different lanes
// producing the same tx), BlockDB returns every recorded execution; we
// pick the canonical one here. Order of preference:
//  1. The lowest-height execution with Code == abci.CodeTypeOK (a tx is
//     expected to succeed at most once across the chain).
//  2. Otherwise the highest-height failure (most recent attempt).
//  3. If no executions are recorded but the tx hash is known to BlockDB,
//     return ErrTxResultPending — distinguishes "may retry" from
//     "definitely doesn't exist".
func (r *GigaRouter) Tx(ctx context.Context, hash []byte) (*coretypes.ResultTx, error) {
	tx, results, found, err := r.blockDB.GetTransactionByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("blockDB.GetTransactionByHash: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("tx (%X) not found", hash)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("tx (%X): %w", hash, ErrTxResultPending)
	}

	// Pick the canonical execution. Unmarshal each result once to read
	// Code; the multi-result case is rare so the per-call cost is small.
	var (
		successful *abci.ExecTxResult
		successRes block.Result
		failure    *abci.ExecTxResult
		failureRes block.Result
	)
	for _, res := range results {
		var parsed abci.ExecTxResult
		if err := parsed.Unmarshal(res.Bytes()); err != nil {
			return nil, fmt.Errorf("unmarshal tx result (block height %d): %w", res.Height(), err)
		}
		if parsed.Code == abci.CodeTypeOK {
			if successful == nil || res.Height() < successRes.Height() {
				p := parsed
				successful = &p
				successRes = res
			}
			continue
		}
		if failure == nil || res.Height() > failureRes.Height() {
			p := parsed
			failure = &p
			failureRes = res
		}
	}

	chosenResult := successful
	chosenRes := successRes
	if chosenResult == nil {
		chosenResult = failure
		chosenRes = failureRes
	}
	return &coretypes.ResultTx{
		Hash:     hash,
		Height:   utils.Clamp[int64](chosenRes.Height()),
		Index:    chosenRes.Index(),
		TxResult: *chosenResult,
		Tx:       tx.Bytes(),
	}, nil
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
	return r.translateBlock(newGlobalBlockAdapter(gb)), nil
}

// BlockByHash returns the finalized global block keyed by Autobahn block-
// header hash, translated into the CometBFT coretypes.ResultBlock shape
// (same translation as BlockByNumber). Matches CometBFT semantics for
// unknown hashes: returns &ResultBlock{Block: nil} with no error.
//
// Reads from sei-db/ledger_db/block.BlockDB, which runExecute populates
// just before each block is handed to the app. Blocks finalized but not
// yet started executing are not yet indexed and read as "unknown" — same
// shape CometBFT returns for an unknown hash. Wrong-size inputs are
// rejected at the call site (env.BlockByHash) so this method can stay
// strongly typed on atypes.BlockHeaderHash.
func (r *GigaRouter) BlockByHash(ctx context.Context, hash atypes.BlockHeaderHash) (*coretypes.ResultBlock, error) {
	b, ok, err := r.blockDB.GetBlockByHash(ctx, hash.Bytes())
	if err != nil {
		return nil, fmt.Errorf("blockDB.GetBlockByHash: %w", err)
	}
	if !ok {
		return &coretypes.ResultBlock{}, nil
	}
	return r.translateBlock(b), nil
}

// translateBlock converts a block.Block into the CometBFT coretypes.ResultBlock
// shape used by env.Block / env.BlockByHash and downstream evmrpc consumers.
// Both BlockByNumber (data.State path, wrapped via globalBlockAdapter) and
// BlockByHash (BlockDB path) feed through here so the read path always emits
// the same shape regardless of source.
//
// LastCommit is non-nil with empty Signatures, mirroring executeBlock's
// FinalizeBlock call which passes an empty abci.CommitInfo. Under Autobahn
// the committee is fixed by genesis (no validator-set updates), so the
// application is not in control of jailing — surfacing N "absent sig"
// entries here would make trace replay's BeginBlock bump missed-block
// counters and diverge from production. ToReqBeginBlock skips the per-
// validator loop when Signatures is empty, so empty Votes flow into
// distribution/slashing on both paths.
func (r *GigaRouter) translateBlock(b block.Block) *coretypes.ResultBlock {
	srcTxs := b.Transactions()
	tmTxs := make(types.Txs, len(srcTxs))
	for i, tx := range srcTxs {
		tmTxs[i] = tx.Bytes()
	}
	return &coretypes.ResultBlock{
		BlockID: types.BlockID{Hash: tmbytes.HexBytes(b.Hash())},
		Block: &types.Block{
			Header: types.Header{
				ChainID: r.cfg.GenDoc.ChainID,
				// Clamp accepts any constraints.Integer for From, so the
				// uint64 height goes in directly — no intermediate cast.
				Height: utils.Clamp[int64](b.Height()),
				Time:   b.Time(),
			},
			Data:       types.Data{Txs: tmTxs},
			LastCommit: &types.Commit{},
		},
	}
}

func (r *GigaRouter) executeBlock(ctx context.Context, b *atypes.GlobalBlock) (*abci.ResponseCommit, []*abci.ExecTxResult, error) {
	app := r.cfg.TxMempool.App()
	hash := b.Header.Hash()
	var proposerAddress types.Address
	if vals := app.GetValidators(); len(vals) > 0 {
		// Deterministically select a proposer from the app's validator committee.
		// We need it so that app does not emit error logs.
		proposer := slices.MinFunc(vals, func(a, b abci.ValidatorUpdate) int { return a.PubKey.Compare(b.PubKey) })
		key, err := crypto.PubKeyFromProto(proposer.PubKey)
		if err != nil {
			return nil, nil, fmt.Errorf("crypto.PubKeyFromProto(): %w", err)
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
		return nil, nil, fmt.Errorf("r.cfg.App.FinalizeBlock(): %w", err)
	}
	if err := r.data.PushAppHash(ctx, b.GlobalNumber, resp.AppHash); err != nil {
		return nil, nil, fmt.Errorf("r.data.PushAppHash(%v): %w", b.GlobalNumber, err)
	}
	commitResp, err := app.Commit(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("r.cfg.App.Commit(): %w", err)
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
		return nil, nil, fmt.Errorf("r.cfg.TxMempool.Update(%v): %w", b.GlobalNumber, err)
	}
	return commitResp, resp.TxResults, nil
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
		// Persist to BlockDB before execution. WriteBlock provides
		// read-your-writes within this process, so any concurrent RPC
		// BlockByHash sees the block from this point forward. The data
		// layer's WAL remains the primary durability story; BlockDB is the
		// hash index, not the source of truth on restart.
		if err := r.blockDB.WriteBlock(ctx, newGlobalBlockAdapter(b)); err != nil {
			return fmt.Errorf("r.blockDB.WriteBlock(%v): %w", n, err)
		}
		commitResp, txResults, err := r.executeBlock(ctx, b)
		if err != nil {
			return fmt.Errorf("r.executeBlock(%v): %w", n, err)
		}
		// Attach per-tx execution results to the BlockDB entry written
		// above, so RPC consumers (env.Tx) can return them by tx hash.
		// Wrapping each *abci.ExecTxResult in execResultAdapter keeps
		// sei-db chain-agnostic — marshaling happens inside the adapter.
		// Result.Height/Index reflect this block's height + the tx's
		// position so per-block-instance metadata travels with the result
		// (the same tx hash can land in different positions across lane
		// blocks).
		blockHash := b.Header.Hash()
		results := make([]block.Result, len(txResults))
		for i, txResult := range txResults {
			results[i] = execResultAdapter{
				r:      txResult,
				height: uint64(b.GlobalNumber),
				index:  uint32(i), //nolint:gosec // tx index fits in uint32 (block tx count is bounded).
			}
		}
		if err := r.blockDB.SetTransactionResults(ctx, blockHash.Bytes(), results); err != nil {
			return fmt.Errorf("r.blockDB.SetTransactionResults(%v): %w", n, err)
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

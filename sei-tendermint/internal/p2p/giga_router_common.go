package p2p

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"sort"
	"sync/atomic"

	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashvault"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
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

type gigaRouterCommon struct {
	cfg             *GigaRouterCommonConfig
	key             NodeSecretKey
	data            *data.State
	service         *giga.Service
	poolIn          *giga.Pool[NodePublicKey, rpc.Server[giga.API]]
	poolInCommittee *giga.Pool[atypes.PublicKey, rpc.Server[giga.API]]
	poolOut         *giga.Pool[atypes.PublicKey, rpc.Client[giga.API]]
	proxies         utils.RWMutex[map[atypes.PublicKey]*ethrpc.Client]
	app             *proxy.Proxy
	offer           utils.Option[handshakeOffer]
	selfAddr        utils.Option[NodeAddress]
	liveAddrs       utils.RWMutex[map[atypes.PublicKey]GigaNodeAddr]
	liveAddrVersion utils.AtomicSend[uint64]
	// nextCommitEpoch is data.NextCommitEpoch() cached at construction so
	// EvmProxy can Load() without taking the data lock on every call.
	nextCommitEpoch utils.AtomicRecv[*atypes.Epoch]
	// anchor is data.Anchor() cached at construction: the AppQC/CommitQC covering
	// the lowest row data.State still holds.
	anchor utils.AtomicRecv[utils.Option[data.Anchor]]

	// inboundFullnodeCount tracks inbound connections currently served the
	// block-sync subset. Optimistic Add(1) + compare against cap;
	// over-rejects by one or two under contention but never over-accepts.
	inboundFullnodeCount atomic.Int64
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
func BuildDataState(cfg *GigaRouterCommonConfig, blockStore atypes.BlockStore) (*data.State, error) {
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
	if cfg.AppHashStore == nil {
		return nil, errors.New("GigaRouterCommonConfig.AppHashStore is required")
	}
	firstBlock := atypes.GlobalBlockNumber(cfg.GenDoc.InitialHeight) // nolint:gosec // verified to be positive.
	genesisWeights := map[atypes.PublicKey]uint64{}
	for k := range cfg.ValidatorAddrs {
		genesisWeights[k] = 1
	}
	genesisCommittee, err := atypes.NewCommittee(genesisWeights)
	if err != nil {
		return nil, fmt.Errorf("genesis committee: %w", err)
	}
	registry, err := epoch.NewRegistry(genesisCommittee, firstBlock, cfg.GenDoc.GenesisTime, utils.Some(cfg.PersistentStateDir))
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
func (r *gigaRouterCommon) BlockByNumber(ctx context.Context, n atypes.GlobalBlockNumber) (*coretypes.ResultBlock, error) {
	gb, err := r.data.GlobalBlock(ctx, n)
	if err != nil {
		// Map Autobahn's pruning sentinel to CometBFT's, so callers
		// (env.Block, evmrpc, ops tooling) get the same error type they
		// already handle on the CometBFT path. base is None because the
		// active lower bound lives in BlockStore's prune watermark (internal
		// to the store); both call sites format through the same helper.
		if errors.Is(err, atypes.ErrPruned) {
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
// The lookup delegates to data.State.GlobalBlockByHash: an in-memory hash
// index first, then BlockStore for heights evicted after persist. Hashes not yet
// seen or below the prune watermark are read as "unknown". Wrong-size inputs
// are rejected at the call site (env.BlockByHash) so this method can stay
// strongly typed on atypes.BlockHeaderHash.
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
// LastCommit is non-nil with empty Signatures, matching executeBlock's empty CommitInfo.
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

// pendingExecution is what this Commit leaves the hash loop, which it can
// reload neither from the AppHash nor from the data layer: the epoch weights
// as of this Commit, and RetainHeight. The height and the Autobahn block come
// from the AppHash callback.
type pendingExecution struct {
	weights     map[atypes.PublicKey]uint64
	pruneBefore atypes.GlobalBlockNumber
}

// startExecuteBlock finalizes and commits b. AppHash handling stays on the
// hash loop so state hashing can overlap the next consensus wait.
func (r *gigaRouterCommon) startExecuteBlock(
	ctx context.Context,
	b *atypes.GlobalBlock,
) (utils.Option[pendingExecution], error) {
	app := r.app
	hash := b.Header.Hash()
	var proposerAddress types.Address
	if vals := app.GetValidators(); len(vals) > 0 {
		// Deterministically select a proposer from the app's validator committee.
		// We need it so that app does not emit error logs.
		proposer := slices.MinFunc(vals, func(a, b abci.ValidatorUpdate) int { return a.PubKey.Compare(b.PubKey) })
		key, err := crypto.PubKeyFromProto(proposer.PubKey)
		if err != nil {
			return utils.None[pendingExecution](), fmt.Errorf("crypto.PubKeyFromProto(): %w", err)
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
		return utils.None[pendingExecution](), fmt.Errorf("app.FinalizeBlock(): %w", err)
	}
	r.data.PushGasUsed(finalizeBlockGasUsed(resp))

	commitResp, err := app.Commit(ctx)
	if err != nil {
		return utils.None[pendingExecution](), fmt.Errorf("app.Commit(): %w", err)
	}
	pruneBefore, ok := utils.SafeCast[atypes.GlobalBlockNumber](commitResp.RetainHeight)
	if !ok {
		return utils.None[pendingExecution](), fmt.Errorf("invalid commitResp.RetainHeight = %v", commitResp.RetainHeight)
	}
	weights, err := committeeWeights(app.GetValidators())
	if err != nil {
		return utils.None[pendingExecution](), err
	}

	return utils.Some(pendingExecution{
		weights:     weights,
		pruneBefore: pruneBefore,
	}), nil
}

// finishExecuteBlock records and proposes pending's app hash and advances
// durable retention only after the hash has been externalized.
func (r *gigaRouterCommon) finishExecuteBlock(
	ctx context.Context,
	hashVault hashvault.HashVault,
	n atypes.GlobalBlockNumber,
	pending pendingExecution,
	appHash atypes.AppHash,
) error {
	if err := commitAppHashToVault(ctx, hashVault, n, appHash); err != nil {
		return err
	}
	if err := r.data.PushAppHash(ctx, n, appHash, pending.weights); err != nil {
		return fmt.Errorf("r.data.PushAppHash(%v): %w", n, err)
	}
	if err := r.data.PruneBefore(pending.pruneBefore); err != nil {
		return fmt.Errorf("r.data.PruneBefore(%v): %w", pending.pruneBefore, err)
	}
	if err := hashVault.Prune(ctx, uint64(pending.pruneBefore)); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.Info("hashvault prune aborted by context cancellation during shutdown",
				"prune_before", pending.pruneBefore, "err", err)
		} else {
			logger.Error("failed to prune hashvault", "prune_before", pending.pruneBefore, "err", err)
		}
	}
	return nil
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

// gas used, as reported by finalizeBlock() call.
func finalizeBlockGasUsed(resp *abci.ResponseFinalizeBlock) int64 {
	var total int64
	for _, result := range resp.TxResults {
		if result != nil {
			total += max(0, result.GasUsed)
		}
	}
	return total
}

// buildHashVault constructs the app-hash equivocation guard runExecute owns. By default it
// returns a durable Pebble-backed vault rooted at <PersistentStateDir>/hashvault, alongside the
// other Autobahn on-disk state. It returns a no-op vault (no protection) when the operator
// explicitly sets HashVaultDisabledUnsafe, logged loudly.
func buildHashVault(ctx context.Context, cfg *GigaRouterCommonConfig) (hashvault.HashVault, error) {
	if cfg.HashVaultDisabledUnsafe {
		logger.Error("################################################################")
		logger.Error("# HASHVAULT DISABLED (hash-vault-disabled-unsafe=true).        #")
		logger.Error("# This node has NO app-hash equivocation protection and is     #")
		logger.Error("# running in an UNSAFE configuration. Re-enable as soon as the #")
		logger.Error("# underlying issue is resolved.                                #")
		logger.Error("################################################################")
		return hashvault.NewNoopHashVault(), nil
	}
	hvCfg := hashvault.DefaultHashVaultConfig()
	hvCfg.DataDir = filepath.Join(cfg.PersistentStateDir, "hashvault")
	return hashvault.NewPebbleHashVault(ctx, hvCfg)
}

// commitAppHashToVault records the app hash for the given height in the equivocation guard and halts
// the node on any error. Every executed height is guarded, so a node can never commit to two
// different app hashes for the same height without deliberate human intervention.
func commitAppHashToVault(
	ctx context.Context,
	vault hashvault.HashVault,
	height atypes.GlobalBlockNumber,
	hash []byte,
) error {
	err := vault.CommitToHash(ctx, uint64(height), hash)
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		logger.Info("HashVault commit aborted by context cancellation during shutdown; not recording hash",
			"height", height, "err", err)
		return fmt.Errorf("hashvault CommitToHash aborted at height %d: %w", height, err)
	}
	// Build the fatal message once and use it for both the log and the panic. The logger writes
	// directly (no in-process buffer), but a hard crash could still drop the final line, so the
	// panic string carries the full guidance too — panic output is what an operator sees first.
	var msg string
	if errors.Is(err, hashvault.ErrHashMismatch) {
		// The HashVault has already logged the conflicting hashes, its data directory, and the
		// bypass/slashing guidance immediately before returning this error; don't duplicate it.
		msg = fmt.Sprintf("FATAL: HashVault detected an app-hash equivocation at height %d; halting. "+
			"See the preceding HashVault error for the conflicting hashes and recovery steps. "+
			"DO NOT RESTART WITHOUT HUMAN INTERVENTION.", height)
	} else {
		msg = fmt.Sprintf("FATAL: HashVault could not commit the app hash at height %d (operational "+
			"error, not a confirmed equivocation): %v. hashHex=%x. Halting.", height, err, hash)
	}
	logger.Error(msg)
	panic(msg)
}

type appHashStream struct {
	hashes <-chan *lthash.BlockHash
	tip    *lthash.BlockHash
}

// registerAppHashListener subscribes after InitChain and FlushHashes so the
// live stream starts at the next height the execute loop will commit, not at
// a genesis seed or a replay backlog.
func registerAppHashListener(ctx context.Context, store AppHashStore) (appHashStream, error) {
	// One slot: the store publishes a block's hash from inside the execute
	// call that commits it, before that block reaches the hash loop.
	hashes := make(chan *lthash.BlockHash, 1)
	tip, err := store.RegisterHashListener(
		func(_ context.Context, blockNum int64, hash *lthash.BlockHash) error {
			if hash.BlockNumber != blockNum {
				return fmt.Errorf("state store hashed block %d with callback height %d", hash.BlockNumber, blockNum)
			}
			if err := utils.Send(ctx, hashes, hash); err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		},
	)
	if err != nil {
		return appHashStream{}, err
	}
	return appHashStream{hashes: hashes, tip: &tip}, nil
}

// appHashFromState binds a state checksum to the Autobahn block at the height
// the hash itself reports.
func appHashFromState(
	block *atypes.GlobalBlock,
	stateHash *lthash.BlockHash,
) (atypes.AppHash, error) {
	if stateHash.Error != nil {
		return nil, fmt.Errorf("state store hash for block %d: %w", stateHash.BlockNumber, stateHash.Error)
	}
	n, ok := utils.SafeCast[atypes.GlobalBlockNumber](stateHash.BlockNumber)
	if !ok {
		return nil, fmt.Errorf("state store block number %d is not a global height", stateHash.BlockNumber)
	}
	if block.GlobalNumber != n {
		return nil, fmt.Errorf(
			"state store hashed block %d but data layer returned block %d",
			n, block.GlobalNumber,
		)
	}
	checksum := stateHash.Global.Checksum()
	blockHash := block.Header.Hash()
	h := sha256.New()
	_, _ = h.Write(binary.BigEndian.AppendUint64(nil, uint64(n)))
	_, _ = h.Write(blockHash[:])
	_, _ = h.Write(checksum[:])
	return h.Sum(nil), nil
}

func (r *gigaRouterCommon) runExecute(ctx context.Context) error {
	// runExecute is spawned by both router roles and owns the guard until both
	// the ABCI execution loop and the hash-processing loop have stopped.
	hashVault, err := buildHashVault(ctx, r.cfg)
	if err != nil {
		return fmt.Errorf("buildHashVault(): %w", err)
	}
	defer func() {
		if err := hashVault.Close(context.Background()); err != nil {
			logger.Error("failed to close hashvault", "err", err)
		}
	}()

	next, lastBlock, err := r.openApp(ctx)
	if err != nil {
		return err
	}
	// Drain hashing of every already-committed height before we subscribe, so
	// the registration tip is the app tip and the live stream does not replay it.
	if err := r.cfg.AppHashStore.FlushHashes(); err != nil {
		return fmt.Errorf("AppHashStore.FlushHashes(): %w", err)
	}
	appHashes, err := registerAppHashListener(ctx, r.cfg.AppHashStore)
	if err != nil {
		return fmt.Errorf("registerAppHashListener(): %w", err)
	}
	if lastBlock != nil {
		if err := r.backfillAppHashes(ctx, hashVault, lastBlock, appHashes.tip); err != nil {
			return err
		}
	}
	// Unbuffered: execute may commit one block while the hash loop records its
	// predecessor, and blocks on the handoff rather than running further ahead.
	committed := make(chan pendingExecution)
	return scope.Run(ctx, func(scopeCtx context.Context, s scope.Scope) error {
		// Keep the hash loop on runExecute's context so an execute-loop failure
		// does not cancel a committed block while it is being recorded.
		s.SpawnNamed("appHashes", func() error {
			return r.runAppHashes(ctx, hashVault, committed, appHashes.hashes, next)
		})
		s.SpawnNamed("executeBlocks", func() error {
			defer close(committed)
			return r.executeBlocks(scopeCtx, committed, next)
		})
		return nil
	})
}

// openApp brings the ABCI app to a state the execute loop can extend: InitChain
// on a fresh app, or the last committed header on restart. It does not
// subscribe to hashes; InitChain seeds the store at InitialHeight-1.
func (r *gigaRouterCommon) openApp(ctx context.Context) (atypes.GlobalBlockNumber, *atypes.GlobalBlock, error) {
	app := r.app
	info := app.Info()
	last, ok := utils.SafeCast[atypes.GlobalBlockNumber](info.LastBlockHeight)
	if !ok {
		return 0, nil, fmt.Errorf("invalid info.LastBlockHeight = %v", info.LastBlockHeight)
	}
	if last == 0 {
		// Fresh start: CometBFT handshaker is skipped in giga mode (see
		// node.go: shouldHandshake = !stateSync && !gigaEnabled), so we
		// call InitChain ourselves. It sets up the app's deliverState
		// against which the first FinalizeBlock below runs.
		//
		// Re-entering on restart (crashed after InitChain, before first
		// Commit) is safe — nothing was committed, so it behaves as a
		// fresh init.
		if _, err := app.InitChain(r.cfg.GenDoc.ToRequestInitChain()); err != nil {
			return 0, nil, fmt.Errorf("App.InitChain(): %w", err)
		}
		next, ok := utils.SafeCast[atypes.GlobalBlockNumber](r.cfg.GenDoc.InitialHeight)
		if !ok {
			return 0, nil, fmt.Errorf("invalid GenDoc.InitialHeight = %v", r.cfg.GenDoc.InitialHeight)
		}
		return next, nil, nil
	}
	// BuildDataState caps recovery at BlockStore's durable block tip, so a crash
	// after app.Commit but before the BlockStore flush resumes by syncing the
	// missing suffix. If retention instead passed the app tip, GlobalBlock
	// returns ErrPruned here. A readable tip restores the last header and
	// replays AppHash.
	b, err := r.data.GlobalBlock(ctx, last)
	if err != nil {
		if errors.Is(err, atypes.ErrPruned) {
			return 0, nil, fmt.Errorf("app tip %d is unavailable in BlockStore; restore matching BlockStore data or state-sync the node: %w", last, err)
		}
		return 0, nil, fmt.Errorf("r.data.GlobalBlock(): %w", err)
	}
	app.InitLastHeader((&types.Header{
		ChainID: r.cfg.GenDoc.ChainID,
		Height:  int64(b.GlobalNumber), // nolint:gosec // different representations of the same value
		Time:    b.Timestamp,
		// TODO: for consistency we should also set proposerAddress here,
		// but this is a placeholder solution so maybe we don't care.
	}).ToProto())
	return last + 1, b, nil
}

// backfillAppHashes gives the data layer the AppHash of every block the app has
// already committed but has not yet proposed, oldest first, so execution resumes
// at the block after the app tip. PushAppHash rejects an AppHash that skips a
// CommitQC range, so a height left behind here is not recoverable later.
func (r *gigaRouterCommon) backfillAppHashes(
	ctx context.Context,
	hashVault hashvault.HashVault,
	lastBlock *atypes.GlobalBlock,
	stateTip *lthash.BlockHash,
) error {
	weights, err := committeeWeights(r.app.GetValidators())
	if err != nil {
		return err
	}
	for n := r.data.NextAppProposal(); n <= lastBlock.GlobalNumber; n++ {
		appHash, err := r.recoverAppHash(ctx, hashVault, n, lastBlock, stateTip)
		if err != nil {
			return err
		}
		if err := r.data.PushAppHash(ctx, n, appHash, weights); err != nil {
			return fmt.Errorf("r.data.PushAppHash(%v): %w", n, err)
		}
	}
	return nil
}

// recoverAppHash returns the AppHash this node committed to at already-executed
// block n. The hashvault is that record, and holds every height whose hash was
// recorded before the node stopped.
//
// Only the app tip can be missing, because the hash is derived from committed
// state and so reaches the vault after the commit it describes. That one height
// is also the only one the state store can still answer for, since it publishes
// the hash of its own tip.
func (r *gigaRouterCommon) recoverAppHash(
	ctx context.Context,
	hashVault hashvault.HashVault,
	n atypes.GlobalBlockNumber,
	lastBlock *atypes.GlobalBlock,
	stateTip *lthash.BlockHash,
) (atypes.AppHash, error) {
	appHash, ok, err := hashVault.CommittedHash(ctx, uint64(n))
	if err != nil {
		return nil, fmt.Errorf("hashVault.CommittedHash(%v): %w", n, err)
	}
	if ok {
		return appHash, nil
	}
	if n != lastBlock.GlobalNumber {
		return nil, fmt.Errorf(
			"hashvault holds no AppHash for committed block %v, and only the app tip %v can be re-derived from state",
			n, lastBlock.GlobalNumber,
		)
	}
	appHash, err = appHashFromState(lastBlock, stateTip)
	if err != nil {
		return nil, err
	}
	// Record it now, so the guard covers this height as it would have had the
	// node not stopped between committing the block and recording its hash.
	if err := commitAppHashToVault(ctx, hashVault, n, appHash); err != nil {
		return nil, err
	}
	return appHash, nil
}

// runAppHashes waits for each committed block's store hash, then records,
// proposes, and prunes that block. It drains every block handed off before the
// execute loop closes committed. Hashes below first are leftover seed or replay
// and are discarded so pairing stays aligned with execute.
func (r *gigaRouterCommon) runAppHashes(
	ctx context.Context,
	hashVault hashvault.HashVault,
	committed <-chan pendingExecution,
	hashes <-chan *lthash.BlockHash,
	first atypes.GlobalBlockNumber,
) error {
	for {
		p, ok, err := utils.RecvOrClosed(ctx, committed)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		stateHash, err := recvHashAtLeast(ctx, hashes, first)
		if err != nil {
			return err
		}
		n, ok := utils.SafeCast[atypes.GlobalBlockNumber](stateHash.BlockNumber)
		if !ok {
			return fmt.Errorf("state store block number %d is not a global height", stateHash.BlockNumber)
		}
		block, err := r.data.GlobalBlock(ctx, n)
		if err != nil {
			return fmt.Errorf("r.data.GlobalBlock(%v): %w", n, err)
		}
		appHash, err := appHashFromState(block, stateHash)
		if err != nil {
			return err
		}
		if err := r.finishExecuteBlock(ctx, hashVault, n, p, appHash); err != nil {
			return fmt.Errorf("r.finishExecuteBlock(%v): %w", n, err)
		}
		first = n + 1
	}
}

// recvHashAtLeast returns the store hash of first, skipping any leftover seed
// or replay of heights already committed.
func recvHashAtLeast(
	ctx context.Context,
	hashes <-chan *lthash.BlockHash,
	first atypes.GlobalBlockNumber,
) (*lthash.BlockHash, error) {
	for {
		stateHash, err := utils.Recv(ctx, hashes)
		if err != nil {
			return nil, err
		}
		n, ok := utils.SafeCast[atypes.GlobalBlockNumber](stateHash.BlockNumber)
		if !ok {
			return nil, fmt.Errorf("state store block number %d is not a global height", stateHash.BlockNumber)
		}
		if n < first {
			continue
		}
		if n != first {
			return nil, fmt.Errorf("state store hashed block %d, want %d", n, first)
		}
		return stateHash, nil
	}
}

// executeBlocks keeps FinalizeBlock and Commit together on this loop, one
// block at a time from next, handing each committed block to the hash loop.
// It never waits on the hash loop's progress: a hash that is slow to arrive
// must not stop the chain from executing.
func (r *gigaRouterCommon) executeBlocks(
	ctx context.Context,
	committed chan<- pendingExecution,
	next atypes.GlobalBlockNumber,
) error {
	for n := next; ; n += 1 {
		b, err := r.data.GlobalBlock(ctx, n)
		if err != nil {
			return fmt.Errorf("r.data.GlobalBlock(%v): %w", n, err)
		}
		opt, err := r.startExecuteBlock(ctx, b)
		if err != nil {
			return fmt.Errorf("r.startExecuteBlock(%v): %w", n, err)
		}
		p := opt.OrPanic("successful block execution returned no pending block")
		if err := utils.Send(ctx, committed, p); err != nil {
			return err
		}
	}
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
		// TODO: handshake needs a timeout.
		hConn, err := handshake(ctx, tcpConn, r.key, handshakeSpec{
			SelfAddr:          r.selfAddr,
			SeiGigaConnection: true,
		}, r.offer)
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

// keepReplicas is commitEpoch's committee, plus the committee Anchor covers when present.
func keepReplicas(anchor utils.Option[data.Anchor], commitEpoch *atypes.Epoch) map[atypes.PublicKey]struct{} {
	keep := map[atypes.PublicKey]struct{}{}
	for lane := range commitEpoch.Committee().Lanes().All() {
		keep[lane.Validator] = struct{}{}
	}
	if a, ok := anchor.Get(); ok {
		for lane := range a.Epoch.Committee().Lanes().All() {
			keep[lane.Validator] = struct{}{}
		}
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
	if !r.nextCommitEpoch.Load().Committee().HasReplica(claim.Validator) {
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
	anchor utils.Option[data.Anchor],
	commitEpoch *atypes.Epoch,
) error {
	a, hasAnchor := anchor.Get()
	// Until Anchor is within one epoch, validators of the epochs in between are
	// in neither endpoint committee, and the AppQCs for those epochs cannot form
	// without them.
	settled := hasAnchor && commitEpoch.EpochIndex() <= a.Epoch.EpochIndex()+1
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
		epochOf := func(opt utils.Option[data.Anchor]) utils.Option[atypes.EpochIndex] {
			return utils.MapOpt(opt, func(a data.Anchor) atypes.EpochIndex { return a.Epoch.EpochIndex() })
		}
		for ctx.Err() == nil {
			addrVersion := addrUpdates.Load()
			commitEpoch := r.nextCommitEpoch.Load()
			anchor := r.anchor.Load()
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
				return r.nextCommitEpoch.Load().EpochIndex() != commitEpoch.EpochIndex() ||
					epochOf(r.anchor.Load()) != epochOf(anchor) ||
					addrUpdates.Load() != addrVersion
			}, r.nextCommitEpoch, r.anchor, addrUpdates); err != nil {
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
			_, err := r.nextCommitEpoch.Wait(ctx, func(epoch *atypes.Epoch) bool {
				return !epoch.Committee().HasReplica(validator)
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
	// A member who inbounds before they appear in our nextCommitEpoch view is
	// served as a fullnode for this socket's life; their own dialer redials
	// after DialInterval and re-handshakes into the validator role.
	return r.runInboundFullnode(ctx, hConn)
}

func (r *gigaRouterCommon) runInboundFullnode(ctx context.Context, hConn *handshakedConn) error {
	key := hConn.msg.NodeAuth.Key()
	server := rpc.NewServer[giga.API]()
	// Optimistic acquire: Add(1), compare, Add(-1) on overflow. Acquired
	// before InsertAndRun, which evicts any live connection for this key.
	if r.inboundFullnodeCount.Add(1) > r.inboundFullnodeCap {
		r.inboundFullnodeCount.Add(-1)
		return fmt.Errorf("inbound fullnode peer limit (%d) reached", r.inboundFullnodeCap)
	}
	defer r.inboundFullnodeCount.Add(-1)
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
// Before the first CommitQC, FirstBlock resolves to the genesis committee.
func (r *gigaRouterCommon) Validators(n atypes.GlobalBlockNumber) ([]*types.Validator, atypes.GlobalBlockNumber, error) {
	first := r.data.Registry().FirstBlock()
	qc, err := r.data.TryQC(n)
	var epochIndex atypes.EpochIndex
	if errors.Is(err, atypes.ErrNotFound) && n == first {
		epochIndex = 0
	} else if err != nil {
		return nil, 0, heightLookupError(n, err)
	} else {
		epochIndex = qc.QC().Proposal().EpochIndex()
	}
	ep, err := r.data.Registry().EpochByIndex(epochIndex)
	if err != nil {
		return nil, 0, heightLookupError(n, err)
	}
	vs, err := committeeValidators(ep.Committee())
	return vs, n, err
}

// heightLookupError returns the RPC error for a data lookup at global height n.
func heightLookupError(n atypes.GlobalBlockNumber, err error) error {
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

// committeeWeights maps the bonded validator set after Commit to voting power.
func committeeWeights(vals []abci.ValidatorUpdate) (map[atypes.PublicKey]uint64, error) {
	weights := make(map[atypes.PublicKey]uint64, len(vals))
	for _, v := range vals {
		if v.Power <= 0 {
			continue
		}
		pk, err := crypto.PubKeyFromProto(v.PubKey)
		if err != nil {
			return nil, fmt.Errorf("PubKeyFromProto: %w", err)
		}
		apk, err := atypes.PublicKeyFromBytes(pk.Bytes())
		if err != nil {
			return nil, fmt.Errorf("PublicKeyFromBytes: %w", err)
		}
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

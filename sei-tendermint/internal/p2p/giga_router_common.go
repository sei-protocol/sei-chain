package p2p

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
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
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
)

// maxInboundFullnodePeers caps GigaRouterCommonConfig.MaxInboundFullnodePeers.
// Per-peer cost (~50-100 KB resident + ~8 goroutines + 1 fd) and NIC
// bandwidth bind well before this. Shard via an edge-fullnode tier if
// you need more.
const maxInboundFullnodePeers = 10000

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
	inboundFullnodeCount atomic.Int64
	inboundFullnodeCap   int64
}

// buildDataState validates the common config, constructs the committee, opens
// BlockDB (littblock when PersistentStateDir is set, memblock otherwise), and
// returns an initialised data.State alongside the BlockDB handle. The caller
// owns blockDB and must close it after Run returns; data.State never closes it.
func buildDataState(cfg *GigaRouterCommonConfig) (*data.State, atypes.BlockDB, error) {
	if cfg.GenDoc.InitialHeight < 1 {
		return nil, nil, fmt.Errorf("GenDoc.InitialHeight = %v, want >=1", cfg.GenDoc.InitialHeight)
	}
	if cfg.DialInterval <= 0 {
		return nil, nil, fmt.Errorf("GigaRouterCommonConfig.DialInterval = %v, want > 0", cfg.DialInterval)
	}
	if cfg.MaxInboundFullnodePeers < 0 || cfg.MaxInboundFullnodePeers > maxInboundFullnodePeers {
		return nil, nil, fmt.Errorf("GigaRouterCommonConfig.MaxInboundFullnodePeers = %v, want 0..%v", cfg.MaxInboundFullnodePeers, maxInboundFullnodePeers)
	}
	firstBlock := atypes.GlobalBlockNumber(cfg.GenDoc.InitialHeight) // nolint:gosec // verified to be positive.
	genesisWeights := map[atypes.PublicKey]uint64{}
	for k := range cfg.ValidatorAddrs {
		genesisWeights[k] = 1
	}
	genesisCommittee, err := atypes.NewCommittee(genesisWeights)
	if err != nil {
		return nil, nil, fmt.Errorf("genesis committee: %w", err)
	}
	registry, err := epoch.NewRegistry(genesisCommittee, firstBlock, cfg.GenDoc.GenesisTime)
	if err != nil {
		return nil, nil, fmt.Errorf("epoch.NewRegistry(): %w", err)
	}
	var blockDB atypes.BlockDB
	if dir, ok := cfg.PersistentStateDir.Get(); ok {
		blockCfg, err := littblock.DefaultConfig(filepath.Join(dir, "blockdb"))
		if err != nil {
			return nil, nil, fmt.Errorf("littblock.DefaultConfig: %w", err)
		}
		applyBlockDBConfig(blockCfg, cfg.BlockDB)
		blockDB, err = littblock.NewBlockDB(blockCfg)
		if err != nil {
			return nil, nil, fmt.Errorf("open BlockDB: %w", err)
		}
	} else {
		blockDB = memblock.NewBlockDB()
	}
	ds, err := data.NewState(&data.Config{Registry: registry}, blockDB)
	if err != nil {
		_ = blockDB.Close()
		return nil, nil, fmt.Errorf("data.NewState: %w", err)
	}
	return ds, blockDB, nil
}

// applyBlockDBConfig overlays optional Autobahn overrides onto a littblock
// DefaultConfig. Paths are left untouched.
func applyBlockDBConfig(dst *littblock.LittBlockConfig, src BlockDBConfig) {
	if r, ok := src.Retention.Get(); ok {
		dst.Retention = r
	}
	if p, ok := src.GCPeriod.Get(); ok {
		dst.Litt.GCPeriod = p
	}
	if f, ok := src.Fsync.Get(); ok {
		dst.Litt.Fsync = f
	}
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
// The lookup delegates to data.State.GlobalBlockByHash, which reads from an
// in-memory hash index rebuilt from BlockDB at construction time. Hashes not
// yet seen or below the prune watermark are read as "unknown". Wrong-size inputs are rejected
// at the call site (env.BlockByHash) so this method can stay strongly typed
// on atypes.BlockHeaderHash.
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

func (r *gigaRouterCommon) executeBlock(ctx context.Context, b *atypes.GlobalBlock, hashVault hashvault.HashVault) (*abci.ResponseCommit, error) {
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

	// Commit this height's app hash to the equivocation guard before persisting app state, so the
	// vault always records our commitment to a height before the state it implies is committed (and
	// before the hash is proposed for AppQC voting via PushAppHash below). On restart the block is
	// re-executed and the identical hash is re-committed idempotently. A returned error is a benign
	// shutdown cancellation; genuine faults panic inside the call. See commitAppHashToVault.
	if err := commitAppHashToVault(ctx, hashVault, b.GlobalNumber, resp.AppHash); err != nil {
		return nil, err
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

// buildHashVault constructs the app-hash equivocation guard runExecute owns. By default it
// returns a durable Pebble-backed vault rooted at <PersistentStateDir>/hashvault, alongside the
// other Autobahn on-disk state. It returns a no-op vault (no protection) when PersistentStateDir
// is unset or when the operator explicitly sets HashVaultDisabledUnsafe — both logged loudly.
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
	dir, ok := cfg.PersistentStateDir.Get()
	if !ok {
		logger.Error("################################################################")
		logger.Error("# HASHVAULT DISABLED (PersistentStateDir not set).             #")
		logger.Error("# This node has NO app-hash equivocation protection and is     #")
		logger.Error("# running in an UNSAFE configuration. Set persistent_state_dir #")
		logger.Error("# in the node config to enable protection.                     #")
		logger.Error("################################################################")
		return hashvault.NewNoopHashVault(), nil
	}
	hvCfg := hashvault.DefaultHashVaultConfig()
	hvCfg.DataDir = filepath.Join(dir, "hashvault")
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

func (r *gigaRouterCommon) runExecute(ctx context.Context) error {
	// runExecute is the single block-execution loop spawned by both the validator and fullnode Run
	// methods, so it owns the equivocation guard for both roles: build it here (set before the first
	// executeBlock, the only other reader) and close it on exit.
	hashVault, err := buildHashVault(ctx, r.cfg)
	if err != nil {
		return fmt.Errorf("buildHashVault(): %w", err)
	}
	defer func() {
		if err := hashVault.Close(context.Background()); err != nil {
			logger.Error("failed to close hashvault", "err", err)
		}
	}()

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
		// Re-commit the last finalized block's app hash to the equivocation guard before re-proposing it
		// for AppQC voting (PushAppHash below), mirroring executeBlock's commit-before-PushAppHash
		// ordering. On a normal restart this idempotently matches the hash recorded when `last` was
		// first executed; if the committed app state has diverged from what the vault recorded (e.g. an
		// out-of-band rollback/restore), this halts the node instead of externalizing a conflicting
		// hash. A returned error is a benign shutdown cancellation; genuine faults panic inside.
		if err := commitAppHashToVault(ctx, hashVault, last, info.LastBlockAppHash); err != nil {
			return err
		}
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
		commitResp, err := r.executeBlock(ctx, b, hashVault)
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
		// Align the vault's retention with the data layer's prune boundary.
		if err := hashVault.Prune(ctx, uint64(pruneBefore)); err != nil {
			// A canceled context just means we're shutting down between a successful executeBlock
			// and this prune; that's benign, not a prune failure, so don't alarm operators.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				logger.Info("hashvault prune aborted by context cancellation during shutdown",
					"prune_before", pruneBefore, "err", err)
			} else {
				logger.Error("failed to prune hashvault", "prune_before", pruneBefore, "err", err)
			}
		}
	}
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
				Global.gigaNewConnsAt("out").Add(1)
				Global.gigaConnsAt("out").Add(1)
				defer Global.gigaConnsAt("out").Add(-1)
				return runClient(ctx, client)
			})
		})
	})
}

// RunInboundConn serves an inbound giga connection. Non-committee peers
// get the block-sync subset (StreamFullCommitQCs + GetBlock), capped at
// inboundFullnodeCap. Committee peers get the full RunServer on
// validators; on a fullnode the connection is refused (committee peers
// shouldn't be dialing fullnodes — see Service.RunInbound).
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
			Global.gigaNewConnsAt("in").Add(1)
			Global.gigaConnsAt("in").Add(1)
			defer Global.gigaConnsAt("in").Add(-1)
			if err := r.service.RunInbound(ctx, server, isCommittee); err != nil {
				return fmt.Errorf("inbound from %v: %w", key, err)
			}
			return nil
		})
	})
}

// EvmProxy returns the shard owner's EVMRPC URL for an EVM tx sender, or
// None if the caller should handle it locally. Overridden on
// *gigaValidatorRouter to short-circuit self-shard sends.
func (r *gigaRouterCommon) EvmProxy(sender common.Address) utils.Option[*url.URL] {
	shardValidator := r.data.Registry().LatestEpoch().Committee().EvmShard(sender)
	return utils.Some(r.cfg.ValidatorAddrs[shardValidator].EVMRPC)
}

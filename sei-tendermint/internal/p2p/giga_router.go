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

// GigaRouterCommonConfig is the config slice shared by both router modes.
// Embedded into GigaFullnodeConfig and GigaValidatorConfig.
type GigaRouterCommonConfig struct {
	// DialInterval is the outbound dial-retry sleep on both paths and the
	// initial backoff on the fullnode subscriber. Must be > 0.
	DialInterval time.Duration
	// ValidatorAddrs is the committee membership table — validator key →
	// {node key, host:port, EVMRPC URL}. Every entry must expose a non-None
	// EVMRPC URL on both paths so EvmProxy never silently drops a tx.
	ValidatorAddrs map[atypes.PublicKey]GigaNodeAddr
	// PersistentStateDir is the directory the data layer's WAL writes to
	// (validator's consensus persister piggybacks on the same root via
	// distinct subdirs). None means in-memory only.
	PersistentStateDir utils.Option[string]
	// GenDoc is the genesis document — chain ID, initial height, genesis
	// time, ConsensusParams.Block.MaxGas (the source the fullnode's
	// MaxGasEstimatedPerBlock reads from).
	GenDoc *types.GenesisDoc
}

// GigaFullnodeConfig configures a non-validator GigaRouter. Fullnodes pull
// finalized blocks from committee members via giga block-sync, execute
// them locally, and forward EVM tx writes to the shard owner over
// EvmProxy. They don't run consensus or produce blocks.
type GigaFullnodeConfig struct {
	GigaRouterCommonConfig
	// App is the ABCI application proxy that executeBlock drives. Fullnodes
	// don't carry a producer (which is where validators source App from)
	// so it's passed directly here.
	App *proxy.Proxy
}

// GigaValidatorConfig configures a committee-member GigaRouter. Embeds the
// common config; adds the consensus / producer state validators need
// (autobahn key, view timeout, producer config) plus the inbound-rpc-peer
// cap. The Producer owns the local mempool and the ABCI App proxy;
// executeBlock reads cfg.Producer.App.
type GigaValidatorConfig struct {
	GigaRouterCommonConfig
	// ValidatorKey is the autobahn secret key signing consensus messages
	// and identifying this node in the committee.
	ValidatorKey atypes.SecretKey
	// ViewTimeout maps view → timeout duration for consensus.
	ViewTimeout func(atypes.View) time.Duration
	// Producer configures block production. Required. Carries the ABCI
	// App proxy and gas/tx-per-block caps; the producer constructs its
	// own mempool internally from this config.
	Producer *producer.Config
	// MaxInboundFullnodePeers caps concurrent inbound block-sync
	// connections from non-committee peers. 0 disables inbound fullnode
	// block-sync entirely (rejects all non-committee peers); n > 0 caps
	// at that value. setup.go fills in the operator-facing default from
	// config.DefaultAutobahnMaxInboundFullnodePeers when the TOML key is
	// absent — direct constructions that want the default must populate
	// it themselves. Negative values are rejected at construction.
	MaxInboundFullnodePeers int
}

// GigaRouter is the per-node entry point into the Autobahn stack — the
// read-path / Run / EvmProxy surface that callers in router.go and
// internal/rpc/core/{blocks,status,mempool}.go reach through. Two concrete
// implementations live in this package: *gigaValidatorRouter (committee
// members; produced by NewGigaValidatorRouter) and *gigaFullnodeRouter
// (non-validators; produced by NewGigaFullnodeRouter). Validator-only
// operations (RunInboundConn) live on *gigaValidatorRouter and are reached
// via in-package type assertion rather than an interface method — fullnodes
// can't accept inbound giga connections, so an interface method that
// returned an error per mode would not be type-safe.
type GigaRouter interface {
	Run(ctx context.Context) error
	LastCommittedBlockNumber() int64
	MaxGasEstimatedPerBlock() uint64
	BlockByNumber(ctx context.Context, n atypes.GlobalBlockNumber) (*coretypes.ResultBlock, error)
	BlockByHash(ctx context.Context, hash atypes.BlockHeaderHash) (*coretypes.ResultBlock, error)
	EvmProxy(sender common.Address) (*url.URL, bool)
	// Mempool returns the producer-backed mempool on validators (Some)
	// and None on fullnodes — fullnodes don't accept local CheckTx, every
	// EVM tx is forwarded to the shard owner via EvmProxy. Callers that
	// need to insert/query txs branch on Get(); the "no local mempool"
	// case is a real semantic, not a dummy value.
	Mempool() utils.Option[*producer.State]
}

// gigaRouterCommon holds the GigaRouter fields and methods shared between
// validator and fullnode impls. Both concrete impls embed
// *gigaRouterCommon and inherit the read-path / execute-loop logic from
// it; mode-specific behaviour (Run, LastCommittedBlockNumber,
// MaxGasPerBlock; plus RunInboundConn on the validator only) is
// implemented on the embedding types.
type gigaRouterCommon struct {
	cfg     *GigaRouterCommonConfig
	key     NodeSecretKey
	data    *data.State
	service *giga.Service
	poolOut *giga.Pool[NodePublicKey, rpc.Client[giga.API]]
	// app is the ABCI proxy executeBlock/runExecute drive. Sourced from
	// cfg.Producer.App on validators (the producer owns it) and
	// cfg.App on fullnodes (no producer). Stored on common so the
	// execute loop doesn't need to know which mode it's running in.
	app *proxy.Proxy
	// validatorKey is the autobahn public key of this node when it's a
	// committee member; None on fullnodes. Used by EvmProxy to short-circuit
	// when the sender's shard owner is us (handle locally via mempool
	// instead of HTTP-forwarding to ourselves). Fullnodes never short-circuit
	// because they have no key and no mempool.
	validatorKey utils.Option[atypes.PublicKey]
	// lastExecutedBlock is the highest global block number for which the
	// app has Commit-ed. Read by LastCommittedBlockNumber so /status
	// returns the executed frontier (matching CometBFT semantics) rather
	// than the consensus-QC or data-receive frontier — clients querying
	// receipts/balances at the reported height never see a height the app
	// hasn't reached. runExecute seeds it from app.Info().LastBlockHeight
	// at startup, then updates it after every successful executeBlock.
	lastExecutedBlock atomic.Int64
}

// gigaValidatorRouter is the GigaRouter impl for committee members. It
// runs consensus + producer, accepts inbound giga connections (full
// service for committee peers, block-sync subset for fullnode peers), and
// dials every committee member in parallel for vote fan-out.
type gigaValidatorRouter struct {
	*gigaRouterCommon

	consensus      *consensus.State
	producer       *producer.State
	producerConfig *producer.Config
	poolIn         *giga.Pool[NodePublicKey, rpc.Server[giga.API]]

	// inboundFullnodeCount tracks live non-committee inbound block-sync
	// connections. Acquire-and-check is Add(1) + compare against
	// inboundFullnodeCap; release is Add(-1). A brief overshoot under
	// contention is acceptable — we just over-reject one or two peers,
	// not over-accept — and an atomic counter avoids the queueing
	// behaviour a sync.Mutex or buffered channel would imply.
	inboundFullnodeCount atomic.Int32
	// inboundFullnodeCap is the configured cap, captured in the rejection
	// error and used as the comparison threshold for inboundFullnodeCount.
	inboundFullnodeCap int32
}

// validateCommonAndBuildData runs the validation and data-layer setup
// shared by both NewGigaFullnodeRouter and NewGigaValidatorRouter:
// genesis sanity, dial-interval sanity, committee assembly, the
// every-committee-member-has-an-EVMRPC-URL guard, and data.State
// construction. Returns the constructed data.State so each constructor
// can wrap it differently (block-sync-only service for fullnodes, full
// service for validators).
//
// The EVMRPC check elevated to here means the (nil, false) silent-drop
// path of EvmProxy's .Get() is unreachable in production on either mode:
// fullnodes can't produce blocks locally so EvmProxy is their only outlet,
// and validators silently mempool a tx that should have been forwarded if
// the shard owner's URL is missing.
//
// The data WAL piggybacks on PersistentStateDir; the validator's consensus
// persister uses the same root via distinct subdirs (inner / blocks /
// commitqcs for consensus, globalblocks / fullcommitqcs for data). None
// means in-memory only.
//
// TODO(autobahn): once sei-db/ledger_db/block.BlockDB has a writer wired
// (see BlockByNumber's TODO), the data layer's WAL is redundant —
// BlockDB is the long-term home for the block read path and survives
// process restarts on its own. At that point this NewDataWAL call can
// drop the directory and become a no-op.
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
	// Explicit non-empty guard so direct constructions (bypassing
	// AutobahnFileConfig.Validate) can't reach the runFullnodeSubscriber
	// modulo-by-zero on `(i + 1) % len(addrs)`.
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

// NewGigaFullnodeRouter constructs a non-validator GigaRouter. Returns
// the concrete type rather than the interface so internal callers can
// reach validator-only methods on the validator type without a runtime
// downcast that returns an error.
func NewGigaFullnodeRouter(cfg *GigaFullnodeConfig, key NodeSecretKey) (*gigaFullnodeRouter, error) {
	dataState, err := validateCommonAndBuildData(&cfg.GigaRouterCommonConfig)
	if err != nil {
		return nil, err
	}
	if cfg.App == nil {
		return nil, fmt.Errorf("GigaFullnodeConfig.App must be set")
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

// NewGigaValidatorRouter constructs a committee-member GigaRouter. Returns
// the concrete type so callers in router.go can reach RunInboundConn
// without a runtime cast that returns an error.
func NewGigaValidatorRouter(cfg *GigaValidatorConfig, key NodeSecretKey) (*gigaValidatorRouter, error) {
	dataState, err := validateCommonAndBuildData(&cfg.GigaRouterCommonConfig)
	if err != nil {
		return nil, err
	}
	if cfg.Producer == nil {
		return nil, fmt.Errorf("GigaValidatorConfig.Producer must be set")
	}
	if cfg.Producer.App == nil {
		return nil, fmt.Errorf("GigaValidatorConfig.Producer.App must be set")
	}
	if cfg.ViewTimeout == nil {
		return nil, fmt.Errorf("GigaValidatorConfig.ViewTimeout must be set")
	}
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
			app:          cfg.Producer.App,
			validatorKey: utils.Some(cfg.ValidatorKey.Public()),
		},
		consensus:          consensusState,
		producer:           producerState,
		producerConfig:     cfg.Producer,
		poolIn:             giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
		inboundFullnodeCap: int32(cfg.MaxInboundFullnodePeers), // nolint:gosec // validated >= 0 above.
	}, nil
}

// LastCommittedBlockNumber returns the highest global block number the app
// has Commit-ed. Both validator and fullnode read this from
// lastExecutedBlock, which runExecute updates after each block commit;
// /status therefore reports the executed frontier rather than a
// consensus-QC-only or block-received-only height, matching CometBFT
// semantics so clients querying receipts/balances at the reported height
// never see a height the app hasn't reached. Lock-free.
func (r *gigaRouterCommon) LastCommittedBlockNumber() int64 {
	return r.lastExecutedBlock.Load()
}

// MaxGasEstimatedPerBlock returns the producer's configured max gas per
// block as uint64 (the chain's gas-limit consensus rule, sourced from
// genesis consensus_params.block.max_gas). Read by the RPC layer to
// populate ResultBlockResults.ConsensusParamUpdates under Autobahn (where
// FinalizeBlock responses are not stored on disk). Lock-free.
func (r *gigaValidatorRouter) MaxGasEstimatedPerBlock() uint64 {
	return r.producerConfig.MaxGasEstimatedPerBlock
}

// Mempool returns Some(producer.State) — validators own a producer-backed
// mempool that the RPC layer (broadcast_tx_*, evm tx insertion) reaches
// through this accessor.
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
	// Publish the executed frontier immediately after Commit so concurrent
	// readers of LastCommittedBlockNumber don't see app.LastBlockHeight
	// briefly racing ahead of our reported height while control is still
	// inside executeBlock (e.g. PushAppHash). The narrow window between
	// Commit returning and this Store is nanoseconds — vs the
	// previous "caller-loop publishes" pattern which left the gap open
	// until executeBlock returned.
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
	// Seed lastExecutedBlock from the app's reported height before the
	// executeBlock loop starts so LastCommittedBlockNumber returns the
	// correct value on restart (and during the catch-up phase below it
	// will advance after each Commit).
	r.lastExecutedBlock.Store(info.LastBlockHeight)
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
// fullnode routers run: the data layer, the executeBlock loop, and the
// giga service (block fetcher). Mode-specific spawns (dial loop +
// consensus/producer on the validator path; the single-active subscriber
// on the fullnode path) live at each Run call site.
func (r *gigaRouterCommon) spawnReadPath(ctx context.Context, s scope.Scope) {
	s.SpawnNamed("data", func() error { return r.data.Run(ctx) })
	s.SpawnNamed("execute", func() error { return r.runExecute(ctx) })
	s.SpawnNamed("service", func() error { return r.service.Run(ctx) })
}

// dialAndRunConn dials a committee member, handshakes as a SeiGiga
// connection, registers the resulting rpc client in poolOut, and hands
// off to `runClient` for the per-connection lifetime. The runClient
// callback is the only mode-specific piece (validator uses the full
// service.RunClient; fullnode uses service.RunBlockSyncClient), so the
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
	// is treated as fullnode and gets the block-sync subset
	// (StreamFullCommitQCs + GetBlock), capped at r.inboundFullnodeCap
	// concurrent connections per validator.
	isCommittee := false
	for _, addr := range r.cfg.ValidatorAddrs {
		if addr.Key == key {
			isCommittee = true
			break
		}
	}
	if !isCommittee {
		// Optimistic acquire: increment, check, decrement on overflow.
		// Brief overshoot is benign — under contention we over-reject by
		// one or two peers but never over-accept.
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

// EvmProxy returns the EVMRPC URL of the validator that owns the shard for
// the given sender, or (nil, false) when the shard owner is this node itself
// (validators handle their own shard via the local mempool). Fullnodes have
// no validatorKey so the self-shard check is always false and EvmProxy
// always forwards. NewGigaRouter enforces that every committee member has
// an EVMRPC URL on both paths, so the (nil, false) silent-drop branch from
// .Get() is unreachable in production.
func (r *gigaRouterCommon) EvmProxy(sender common.Address) (*url.URL, bool) {
	shardValidator := r.data.Committee().EvmShard(sender)
	if myKey, ok := r.validatorKey.Get(); ok && myKey == shardValidator {
		return nil, false
	}
	return r.cfg.ValidatorAddrs[shardValidator].EVMRPC.Get()
}

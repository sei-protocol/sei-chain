package state

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/merkle"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/seilog"
	otrace "go.opentelemetry.io/otel/trace"
)

var logger = seilog.NewLogger("tendermint", "internal", "state")

// proposerPriorityHashInterval is how often (in heights) the
// ProposerPriorityHash metric is exported. Used so operators can compare
// hashes across validators and detect ProposerPriority divergence.
const proposerPriorityHashInterval = 1024

//-----------------------------------------------------------------------------
// BlockExecutor handles block execution and state updates.
// It exposes ApplyBlock(), which validates & executes the block, updates state w/ ABCI responses,
// then commits and updates the mempool atomically, then saves state.

// BlockExecutor provides the context and accessories for properly executing a block.
type BlockExecutor struct {
	// save state, validators, consensus params, abci responses here
	store Store

	// use blockstore for the pruning functions.
	blockStore BlockStore

	// execute the app against this
	app *proxy.Proxy

	// events
	eventBus types.BlockEventPublisher

	// manage the mempool lock during commit
	// and update both with block results after commit.
	mempool *mempool.TxMempool
	evpool  EvidencePool

	metrics *Metrics

	// consensusPolicy is a compile-time validation bypass that only takes
	// effect in mock_block_validation builds; production binaries always see
	// the zero-value (no bypass). Distinct from types.SkipLastResultsHashValidation
	// below, which is a runtime atomic.Bool flipped on for the Giga executor.
	consensusPolicy types.ConsensusPolicy

	// cache the verification results over a single height
	cache map[string]struct{}
}

// NewBlockExecutor returns a new BlockExecutor with the passed-in EventBus.
func NewBlockExecutor(
	stateStore Store,
	app *proxy.Proxy,
	pool *mempool.TxMempool,
	evpool EvidencePool,
	blockStore BlockStore,
	eventBus *eventbus.EventBus,
	metrics *Metrics,
	consensusPolicy types.ConsensusPolicy,
) *BlockExecutor {
	return &BlockExecutor{
		eventBus:        eventBus,
		store:           stateStore,
		app:             app,
		mempool:         pool,
		evpool:          evpool,
		metrics:         metrics,
		cache:           make(map[string]struct{}),
		blockStore:      blockStore,
		consensusPolicy: consensusPolicy,
	}
}

func (blockExec *BlockExecutor) Store() Store {
	return blockExec.store
}

// CreateProposalBlock calls state.MakeBlock with evidence from the evpool
// and txs from the mempool. The max bytes must be big enough to fit the commit.
// Up to 1/10th of the block space is allcoated for maximum sized evidence.
// The rest is given to txs, up to the max gas.
//
// Contract: application will not return more bytes than are sent over the wire.
func (blockExec *BlockExecutor) CreateProposalBlock(
	ctx context.Context,
	height int64,
	state State,
	lastCommit *types.Commit,
	proposerAddr []byte,
) (block *types.Block, err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("panic recovered in CreateProposalBlock", "panic", r, "height", height)
			// Convert panic to error
			block = nil
			err = fmt.Errorf("CreateProposalBlock panic recovered: %v", r)
		}
	}()

	maxBytes := state.ConsensusParams.Block.MaxBytes
	maxGas := state.ConsensusParams.Block.MaxGas
	maxGasWanted := state.ConsensusParams.Block.MaxGasWanted

	evidence, evSize := blockExec.evpool.PendingEvidence(state.ConsensusParams.Evidence.MaxBytes)

	// Fetch a limited amount of valid txs
	maxDataBytes := types.MaxDataBytes(maxBytes, evSize, state.Validators.Size())

	txs, _ := blockExec.mempool.ReapTxs(mempool.ReapLimits{
		MaxBytes:        utils.Some(maxDataBytes),
		MaxGasWanted:    utils.Some(maxGasWanted),
		MaxGasEstimated: utils.Some(maxGas),
	}, false)
	block = state.MakeBlock(height, txs, lastCommit, evidence, proposerAddr)
	return block, nil
}

func (blockExec *BlockExecutor) ProcessProposal(
	ctx context.Context,
	block *types.Block,
	state State,
) (bool, error) {
	txs := block.Txs.ToSliceOfBytes()
	resp, err := blockExec.app.ProcessProposal(ctx, &abci.RequestProcessProposal{
		Txs:                 txs,
		ProposedLastCommit:  buildLastCommitInfo(block, blockExec.store, state.InitialHeight),
		ByzantineValidators: block.Evidence.ToABCI(),
		Hash:                block.Header.Hash(),
		Header:              block.Header.ToProto(),
	})
	if err != nil {
		return false, ErrInvalidBlock(err)
	}
	if resp.IsStatusUnknown() {
		panic(fmt.Sprintf("ProcessProposal responded with status %s", resp.Status.String()))
	}

	return resp.IsAccepted(), nil
}

// ValidateBlock validates the given block against the given state.
// If the block is invalid, it returns an error.
// Validation does not mutate state, but does require historical information from the stateDB,
// ie. to verify evidence from a validator at an old height.
func (blockExec *BlockExecutor) ValidateBlock(ctx context.Context, state State, block *types.Block) error {
	hash := block.Hash()
	if _, ok := blockExec.cache[hash.String()]; ok {
		return nil
	}

	err := validateBlock(state, block, blockExec.consensusPolicy)
	if err != nil {
		// Check if this is a LastResultsHash mismatch and log detailed info
		if !types.SkipLastResultsHashValidation.Load() && !bytes.Equal(block.LastResultsHash, state.LastResultsHash) {
			logger.Error("LastResultsHash mismatch detected",
				"height", block.Height,
				"expectedHash", fmt.Sprintf("%X", state.LastResultsHash),
				"gotHash", fmt.Sprintf("%X", block.LastResultsHash),
				"blockHash", fmt.Sprintf("%X", block.Hash()),
				"lastBlockHeight", state.LastBlockHeight,
				"lastBlockID", state.LastBlockID,
				"numTxs", len(block.Txs),
			)
		}
		return fmt.Errorf("validateBlock(): %w", err)
	}

	err = blockExec.evpool.CheckEvidence(ctx, block.Evidence)
	if err != nil {
		return fmt.Errorf("CheckEvidence(): %w", err)
	}

	blockExec.cache[hash.String()] = struct{}{}
	return nil
}

// ApplyBlock validates the block against the state, executes it against the app,
// fires the relevant events, commits the app, and saves the new state and responses.
// It returns the new state.
// It's the only function that needs to be called
// from outside this package to process and commit an entire block.
// It takes a blockID to avoid recomputing the parts hash.
func (blockExec *BlockExecutor) ApplyBlock(ctx context.Context, state State, blockID types.BlockID, block *types.Block, tracer otrace.Tracer) (State, error) {
	if tracer != nil {
		spanCtx, span := tracer.Start(ctx, "cs.state.ApplyBlock")
		ctx = spanCtx
		defer span.End()
	}
	// validate the block if we haven't already
	if err := blockExec.ValidateBlock(ctx, state, block); err != nil {
		return state, ErrInvalidBlock(err)
	}
	startTime := time.Now()
	defer func() {
		blockExec.metrics.BlockProcessingTimeAt().Observe(time.Since(startTime).Seconds())
	}()
	var finalizeBlockSpan otrace.Span = nil
	if tracer != nil {
		_, finalizeBlockSpan = tracer.Start(ctx, "cs.state.ApplyBlock.FinalizeBlock")
		defer finalizeBlockSpan.End()
	}
	txs := block.Txs.ToSliceOfBytes()
	finalizeBlockStartTime := time.Now()
	fBlockRes, err := blockExec.app.FinalizeBlock(
		ctx,
		&abci.RequestFinalizeBlock{
			Txs:                 txs,
			DecidedLastCommit:   buildLastCommitInfo(block, blockExec.store, state.InitialHeight),
			ByzantineValidators: block.Evidence.ToABCI(),
			Hash:                block.Hash(),
			Header:              block.Header.ToProto(),
		},
	)
	blockExec.metrics.FinalizeBlockLatencyAt().Observe(float64(time.Since(finalizeBlockStartTime).Milliseconds()))
	if finalizeBlockSpan != nil {
		finalizeBlockSpan.End()
	}
	if err != nil {
		return state, ErrProxyAppConn(err)
	}

	logger.Info(
		"finalized block",
		"height", block.Height,
		"latency_ms", time.Since(startTime).Milliseconds(),
		"num_txs_res", len(fBlockRes.TxResults),
		"num_val_updates", len(fBlockRes.ValidatorUpdates),
		"block_app_hash", fmt.Sprintf("%X", fBlockRes.AppHash),
	)
	var saveBlockResponseSpan otrace.Span = nil
	if tracer != nil {
		_, saveBlockResponseSpan = tracer.Start(ctx, "cs.state.ApplyBlock.SaveBlockResponse")
		defer saveBlockResponseSpan.End()
	}
	// Save the results before we commit.
	saveBlockResponseTime := time.Now()
	err = blockExec.store.SaveFinalizeBlockResponses(block.Height, fBlockRes)
	blockExec.metrics.SaveBlockResponseLatencyAt().Observe(float64(time.Since(saveBlockResponseTime).Milliseconds()))
	if err != nil && !errors.Is(err, ErrNoFinalizeBlockResponsesForHeight{block.Height}) {
		// It is correct to have an empty ResponseFinalizeBlock for ApplyBlock,
		// but not for saving it to the state store
		return state, err
	}
	if saveBlockResponseSpan != nil {
		saveBlockResponseSpan.End()
	}

	// validate the validator updates and convert to tendermint types
	err = validateValidatorUpdates(fBlockRes.ValidatorUpdates, state.ConsensusParams.Validator)
	if err != nil {
		return state, fmt.Errorf("error in validator updates: %w", err)
	}

	validatorUpdates, err := types.PB2TM.ValidatorUpdates(fBlockRes.ValidatorUpdates)
	if err != nil {
		return state, err
	}
	if len(validatorUpdates) > 0 {
		logger.Debug("updates to validators", "updates", types.ValidatorListString(validatorUpdates))
		blockExec.metrics.ValidatorSetUpdatesAt().Add(1)
	}
	if fBlockRes.ConsensusParamUpdates != nil {
		blockExec.metrics.ConsensusParamUpdatesAt().Add(1)
	}

	// Update the state with the block and responses.
	var updateStateSpan otrace.Span = nil
	if tracer != nil {
		_, updateStateSpan = tracer.Start(ctx, "cs.state.ApplyBlock.UpdateState")
		defer updateStateSpan.End()
	}
	rs, err := abci.MarshalTxResults(fBlockRes.TxResults)
	if err != nil {
		return state, fmt.Errorf("marshaling TxResults: %w", err)
	}
	h := merkle.HashFromByteSlices(rs)

	// Log LastResultsHash computation details for debugging consensus issues
	if len(fBlockRes.TxResults) > 0 {
		logger.Info("LastResultsHash computed",
			"height", block.Height,
			"hash", fmt.Sprintf("%X", h),
			"txCount", len(fBlockRes.TxResults),
		)
		// Log per-tx deterministic fields (Code, Data, GasWanted, GasUsed) for debugging
		for i, txRes := range fBlockRes.TxResults {
			logger.Debug("TxResult for LastResultsHash",
				"height", block.Height,
				"txIndex", i,
				"code", txRes.Code,
				"gasWanted", txRes.GasWanted,
				"gasUsed", txRes.GasUsed,
				"dataLen", len(txRes.Data),
			)
		}
	}

	state, err = state.Update(blockID, &block.Header, h, fBlockRes.ConsensusParamUpdates, validatorUpdates)
	if err != nil {
		return state, fmt.Errorf("commit failed for application: %w", err)
	}
	if updateStateSpan != nil {
		updateStateSpan.End()
	}

	// Export ProposerPriorityHash every proposerPriorityHashInterval heights so
	// operators can detect ProposerPriority divergence between validators by
	// comparing gauge values across nodes at the same height.
	//
	// Why emit the hash as a numeric *value* rather than a label?
	// A label-based design (gauge with hash as label) would create a new
	// Prometheus time series every time the hash changes — since validator
	// priorities change every block, each emission would yield a brand-new
	// series. Over time this accumulates unbounded cardinality in the
	// metrics backend. Exporting as a numeric value keeps cardinality
	// constant at one series per node.
	//
	// Why take only the first 8 bytes?
	// Prometheus gauges are float64, which only represents integers up to
	// 2^53 exactly. We take the first 8 bytes of the SHA-256 hash and cast
	// to float64; the top 11 bits are lost to the mantissa, effectively
	// giving us 53 bits of entropy. Collision probability across 40
	// validators is ~40^2/2^54 ≈ 9e-14, effectively zero.
	//
	// Paired with ProposerPriorityHashHeight so operators know which height
	// the hash corresponds to. A log line also emits the full 32-byte hash
	// for grep-based debugging.
	//
	// Note on restart staleness: Prometheus Gauges live in memory. After a
	// process restart the gauges reset to zero until the next emission at
	// the following multiple of proposerPriorityHashInterval — up to ~8.5
	// min of stale/zero data at Sei's block times. Acceptable for a
	// monitoring signal that is only checked in response to incidents.
	if block.Height%proposerPriorityHashInterval == 0 {
		if full := state.Validators.ProposerPriorityHash(); len(full) >= 8 {
			packed := binary.BigEndian.Uint64(full[:8])
			blockExec.metrics.ProposerPriorityHashAt().Set(float64(packed))
			blockExec.metrics.ProposerPriorityHashHeightAt().Set(block.Height)
			// Log both the full 32-byte hash (for unambiguous comparison)
			// and the packed value (to correlate with the Prometheus gauge).
			logger.Info("proposer priority hash checkpoint",
				"height", block.Height,
				"hash", fmt.Sprintf("%X", full),
				"packed", packed)
		}
	}
	var commitSpan otrace.Span = nil
	if tracer != nil {
		_, commitSpan = tracer.Start(ctx, "cs.state.ApplyBlock.Commit")
		defer commitSpan.End()
	}
	// Lock mempool, commit app state, update mempoool.
	commitStart := time.Now()
	retainHeight, err := blockExec.Commit(ctx, state, block, fBlockRes.TxResults)
	if err != nil {
		return state, fmt.Errorf("commit failed for application: %w", err)
	}
	if commitSpan != nil {
		commitSpan.End()
	}
	if time.Since(commitStart) > 1000*time.Millisecond {
		logger.Info("commit in blockExec",
			"duration", time.Since(commitStart),
			"height", block.Height)
	}

	// Update evpool with the latest state.
	var updateEvpoolSpan otrace.Span = nil
	if tracer != nil {
		_, updateEvpoolSpan = tracer.Start(ctx, "cs.state.ApplyBlock.UpdateEvpool")
		defer updateEvpoolSpan.End()
	}
	blockExec.evpool.Update(ctx, state, block.Evidence)
	if updateEvpoolSpan != nil {
		updateEvpoolSpan.End()
	}

	// Update the app hash and save the state.
	var saveBlockSpan otrace.Span = nil
	if tracer != nil {
		_, saveBlockSpan = tracer.Start(ctx, "cs.state.ApplyBlock.SaveBlock")
		defer saveBlockSpan.End()
	}
	saveBlockTime := time.Now()
	state.AppHash = fBlockRes.AppHash
	if err := blockExec.store.Save(state); err != nil {
		return state, err
	}
	blockExec.metrics.SaveBlockLatencyAt().Observe(float64(time.Since(saveBlockTime).Milliseconds()))
	if saveBlockSpan != nil {
		saveBlockSpan.End()
	}
	// Prune old heights, if requested by ABCI app.
	var pruneBlockSpan otrace.Span = nil
	if tracer != nil {
		_, pruneBlockSpan = tracer.Start(ctx, "cs.state.ApplyBlock.PruneBlock")
		defer pruneBlockSpan.End()
	}
	pruneBlockTime := time.Now()
	if retainHeight > 0 {
		pruned, err := blockExec.pruneBlocks(retainHeight)
		if err != nil {
			logger.Error("failed to prune blocks", "retain_height", retainHeight, "err", err)
		} else {
			logger.Debug("pruned blocks", "pruned", pruned, "retain_height", retainHeight)
		}
	}
	blockExec.metrics.PruneBlockLatencyAt().Observe(float64(time.Since(pruneBlockTime).Milliseconds()))
	if pruneBlockSpan != nil {
		pruneBlockSpan.End()
	}
	// reset the verification cache
	blockExec.cache = make(map[string]struct{})

	// Events are fired after everything else.
	// NOTE: if we crash between Commit and Save, events wont be fired during replay
	var fireEventsSpan otrace.Span = nil
	if tracer != nil {
		_, fireEventsSpan = tracer.Start(ctx, "cs.state.ApplyBlock.FireEvents")
		defer fireEventsSpan.End()
	}
	fireEventsStartTime := time.Now()
	FireEvents(blockExec.eventBus, block, blockID, fBlockRes, validatorUpdates)
	blockExec.metrics.FireEventsLatencyAt().Observe(float64(time.Since(fireEventsStartTime).Milliseconds()))
	if fireEventsSpan != nil {
		fireEventsSpan.End()
	}
	return state, nil
}

// Commit locks the mempool, runs the ABCI Commit message, and updates the
// mempool.
// It returns the result of calling abci.Commit (the AppHash) and the height to retain (if any).
// The Mempool must be locked during commit and update because state is
// typically reset on Commit and old txs must be replayed against committed
// state before new txs are run in the mempool, lest they be invalid.
func (blockExec *BlockExecutor) Commit(
	ctx context.Context,
	state State,
	block *types.Block,
	txResults []*abci.ExecTxResult,
) (int64, error) {
	blockExec.mempool.Lock()
	defer blockExec.mempool.Unlock()

	// Commit block, get hash back
	start := time.Now()
	res, err := blockExec.app.Commit(ctx)
	if err != nil {
		logger.Error("client error during proxyAppConn.Commit", "err", err)
		return 0, err
	}
	blockExec.metrics.ApplicationCommitTimeAt().Observe(float64(time.Since(start)))

	// ResponseCommit has no error code - just data
	logger.Info(
		"committed state",
		"height", block.Height,
		"num_txs", len(block.Txs),
		"block_app_hash", fmt.Sprintf("%X", block.AppHash),
		"time", time.Now().UnixMilli(),
	)

	// Update mempool.
	start = time.Now()
	err = blockExec.mempool.Update(
		ctx,
		block.Height,
		block.Txs,
		txResults,
		TxConstraintsForState(state),
		state.ConsensusParams.ABCI.RecheckTx,
	)
	blockExec.metrics.UpdateMempoolTimeAt().Observe(float64(time.Since(start)))

	return res.RetainHeight, err
}

func (blockExec *BlockExecutor) SafeGetTxsByHashes(txHashes []types.TxHash) (types.Txs, []types.TxHash) {
	return blockExec.mempool.SafeGetTxsForHashes(txHashes)
}

func buildLastCommitInfo(block *types.Block, store Store, initialHeight int64) abci.CommitInfo {
	if block.Height == initialHeight {
		// there is no last commit for the initial height.
		// return an empty value.
		return abci.CommitInfo{}
	}

	lastValSet, err := store.LoadValidators(block.Height - 1)
	if err != nil {
		panic(fmt.Errorf("failed to load validator set at height %d: %w", block.Height-1, err))
	}

	var (
		commitSize = block.LastCommit.Size()
		valSetLen  = len(lastValSet.Validators)
	)

	// Route a commit/validator-set size divergence through the policy; if it does
	// not halt, the votes below are built best-effort, where the per-index
	// Signatures/Validators pairing is only approximate -- acceptable because
	// LastCommitInfo feeds staking rewards/downtime, never the EVM state under audit.
	if commitSize != valSetLen {
		mismatch := fmt.Errorf(
			"commit size (%d) doesn't match validator set length (%d) at height %d: %w",
			commitSize, valSetLen, block.Height, types.ErrLastCommitVerify)
		if err := types.DefaultConsensusPolicy().HandleError(mismatch); err != nil {
			// Dump the full commit + validator set on the (production) panic path
			// only, where it aids post-mortem; the swallow path skips this.
			panic(fmt.Errorf("%w\n\n%v\n\n%v", err, block.LastCommit.Signatures, lastValSet.Validators))
		}
	}

	votes := make([]abci.VoteInfo, valSetLen)
	for i, val := range lastValSet.Validators {
		signedLastBlock := false
		if i < commitSize {
			signedLastBlock = block.LastCommit.Signatures[i].BlockIDFlag != types.BlockIDFlagAbsent
		}
		votes[i] = abci.VoteInfo{
			Validator:       types.TM2PB.Validator(val),
			SignedLastBlock: signedLastBlock,
		}
	}

	return abci.CommitInfo{
		Round: block.LastCommit.Round,
		Votes: votes,
	}
}

func validateValidatorUpdates(abciUpdates []abci.ValidatorUpdate, params types.ValidatorParams) error {
	for _, valUpdate := range abciUpdates {
		if valUpdate.GetPower() < 0 {
			return fmt.Errorf("voting power can't be negative %v", valUpdate)
		} else if valUpdate.GetPower() == 0 {
			// continue, since this is deleting the validator, and thus there is no
			// pubkey to check
			continue
		}

		// Check if validator's pubkey matches an ABCI type in the consensus params
		pk, err := crypto.PubKeyFromProto(valUpdate.PubKey)
		if err != nil {
			return err
		}

		if !params.IsValidPubkeyType(pk.Type()) {
			return fmt.Errorf("validator %v is using pubkey %s, which is unsupported for consensus",
				valUpdate, pk.Type())
		}
	}
	return nil
}

// Update returns a copy of state with the fields set using the arguments passed in.
func (state State) Update(
	blockID types.BlockID,
	header *types.Header,
	resultsHash []byte,
	consensusParamUpdates *tmtypes.ConsensusParams,
	validatorUpdates []*types.Validator,
) (State, error) {

	// Copy the valset so we can apply changes from FinalizeBlock
	// and update s.LastValidators and s.Validators.
	nValSet := state.NextValidators.Copy()

	// Update the validator set with the latest responses to FinalizeBlock.
	lastHeightValsChanged := state.LastHeightValidatorsChanged
	if len(validatorUpdates) > 0 {
		err := nValSet.UpdateWithChangeSet(validatorUpdates)
		if err != nil {
			return state, fmt.Errorf("changing validator set: %w", err)
		}
		// Change results from this height but only applies to the next next height.
		lastHeightValsChanged = header.Height + 1 + 1
	}

	// Update validator proposer priority and set state variables.
	nValSet.IncrementProposerPriority(1)

	// Update the params with the latest responses to FinalizeBlock.
	nextParams := state.ConsensusParams
	lastHeightParamsChanged := state.LastHeightConsensusParamsChanged
	if consensusParamUpdates != nil {
		// NOTE: must not mutate state.ConsensusParams
		nextParams = state.ConsensusParams.UpdateConsensusParams(consensusParamUpdates)
		err := nextParams.ValidateConsensusParams()
		if err != nil {
			return state, fmt.Errorf("updating consensus params: %w", err)
		}

		err = state.ConsensusParams.ValidateUpdate(consensusParamUpdates, header.Height)
		if err != nil {
			return state, fmt.Errorf("updating consensus params: %w", err)
		}

		state.Version.Consensus.App = nextParams.Version.AppVersion

		// Change results from this height but only applies to the next height.
		lastHeightParamsChanged = header.Height + 1
	}

	nextVersion := state.Version

	// NOTE: the AppHash has not been populated.
	// It will be filled on state.Save.
	return State{
		Version:                          nextVersion,
		ChainID:                          state.ChainID,
		InitialHeight:                    state.InitialHeight,
		LastBlockHeight:                  header.Height,
		LastBlockID:                      blockID,
		LastBlockTime:                    header.Time,
		NextValidators:                   nValSet,
		Validators:                       state.NextValidators.Copy(),
		LastValidators:                   state.Validators.Copy(),
		LastHeightValidatorsChanged:      lastHeightValsChanged,
		ConsensusParams:                  nextParams,
		LastHeightConsensusParamsChanged: lastHeightParamsChanged,
		LastResultsHash:                  resultsHash,
		AppHash:                          nil,
	}, nil
}

// Fire NewBlock, NewBlockHeader.
// Fire TxEvent for every tx.
// NOTE: if Tendermint crashes before commit, some or all of these events may be published again.
func FireEvents(
	eventBus types.BlockEventPublisher,
	block *types.Block,
	blockID types.BlockID,
	finalizeBlockResponse *abci.ResponseFinalizeBlock,
	validatorUpdates []*types.Validator,
) {
	if err := eventBus.PublishEventNewBlock(types.EventDataNewBlock{
		Block:               block,
		BlockID:             blockID,
		ResultFinalizeBlock: *finalizeBlockResponse,
	}); err != nil {
		logger.Error("failed publishing new block", "err", err)
	}

	if err := eventBus.PublishEventNewBlockHeader(types.EventDataNewBlockHeader{
		Header:              block.Header,
		NumTxs:              int64(len(block.Txs)),
		ResultFinalizeBlock: *finalizeBlockResponse,
	}); err != nil {
		logger.Error("failed publishing new block header", "err", err)
	}

	if len(block.Evidence) != 0 {
		for _, ev := range block.Evidence {
			if err := eventBus.PublishEventNewEvidence(types.EventDataNewEvidence{
				Evidence: ev,
				Height:   block.Height,
			}); err != nil {
				logger.Error("failed publishing new evidence", "err", err)
			}
		}
	}

	// sanity check
	if len(finalizeBlockResponse.TxResults) != len(block.Txs) {
		panic(fmt.Sprintf("number of TXs (%d) and ABCI TX responses (%d) do not match",
			len(block.Txs), len(finalizeBlockResponse.TxResults)))
	}

	for i, tx := range block.Txs {
		if err := eventBus.PublishEventTx(types.EventDataTx{
			TxResultV2: abci.TxResultV2{
				Height: block.Height,
				Index:  uint32(i), //nolint:gosec // i is bounded by block.Txs length which fits in uint32
				Tx:     tx,
				Result: *(finalizeBlockResponse.TxResults[i]),
			},
		}); err != nil {
			logger.Error("failed publishing event TX", "err", err)
		}
	}

	if len(finalizeBlockResponse.ValidatorUpdates) > 0 {
		if err := eventBus.PublishEventValidatorSetUpdates(
			types.EventDataValidatorSetUpdates{ValidatorUpdates: validatorUpdates}); err != nil {
			logger.Error("failed publishing event", "err", err)
		}
	}
}

//----------------------------------------------------------------------------------------------------
// Execute block without state. TODO: eliminate

// ExecCommitBlock executes and commits a block on the proxyApp without validating or mutating the state.
// It returns the application root hash (result of abci.Commit).
func ExecCommitBlock(
	ctx context.Context,
	be *BlockExecutor,
	appConn *proxy.Proxy,
	block *types.Block,
	store Store,
	initialHeight int64,
	s State,
) ([]byte, error) {
	finalizeBlockResponse, err := appConn.FinalizeBlock(
		ctx,
		&abci.RequestFinalizeBlock{
			Txs:                 block.Txs.ToSliceOfBytes(),
			DecidedLastCommit:   buildLastCommitInfo(block, store, initialHeight),
			ByzantineValidators: block.Evidence.ToABCI(),
			Hash:                block.Hash(),
			Header:              block.Header.ToProto(),
		},
	)

	if err != nil {
		logger.Error("executing block", "err", err)
		return nil, err
	}
	logger.Info("executed block", "height", block.Height)

	// the BlockExecutor condition is using for the final block replay process.
	if be != nil {
		err = validateValidatorUpdates(finalizeBlockResponse.ValidatorUpdates, s.ConsensusParams.Validator)
		if err != nil {
			logger.Error("validating validator updates", "err", err)
			return nil, err
		}
		validatorUpdates, err := types.PB2TM.ValidatorUpdates(finalizeBlockResponse.ValidatorUpdates)
		if err != nil {
			logger.Error("converting validator updates to native types", "err", err)
			return nil, err
		}

		bps, err := block.MakePartSet(types.BlockPartSizeBytes)
		if err != nil {
			return nil, err
		}

		blockID := types.BlockID{Hash: block.Hash(), PartSetHeader: bps.Header()}
		FireEvents(be.eventBus, block, blockID, finalizeBlockResponse, validatorUpdates)
	}

	// Commit block
	_, err = appConn.Commit(ctx)
	if err != nil {
		logger.Error("client error during proxyAppConn.Commit", "err", err)
		return nil, err
	}

	// ResponseCommit has no error or log
	return finalizeBlockResponse.AppHash, nil
}

func (blockExec *BlockExecutor) pruneBlocks(retainHeight int64) (uint64, error) {
	base := blockExec.blockStore.Base()
	if retainHeight <= base {
		return 0, nil
	}
	pruned, err := blockExec.blockStore.PruneBlocks(retainHeight)
	if err != nil {
		return 0, fmt.Errorf("failed to prune block store: %w", err)
	}

	err = blockExec.Store().PruneStates(retainHeight)
	if err != nil {
		return 0, fmt.Errorf("failed to prune state store: %w", err)
	}
	return pruned, nil
}

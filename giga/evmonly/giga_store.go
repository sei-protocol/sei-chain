package evmonly

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	gigametrics "github.com/sei-protocol/sei-chain/giga/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

const maxGigaStoreBlockNumber = uint64(1<<63 - 1)

var (
	errMissingStateStore            = errors.New("executor requires a state store")
	errMissingNamedChangeSetEncoder = errors.New("giga store requires a named changeset encoder")
	errBlockEncoderUsedEVMStoreKey  = errors.New("block changeset encoder may not write the EVM state changeset")
)

var _ StateReader = gigaSnapshotStateReader{}

// historyStore is a giga store that takes a block's change set alongside its changesets, to keep the
// values the block replaced.
type historyStore interface {
	CommitStateChangesWithPrior(blockNum int64, changeset []*proto.NamedChangeSet, changes *gigatypes.StateChangeSet) error
}

// NamedChangeSetEncoder converts an executor-native state result into the on-disk changesets
// understood by a giga store. It must treat the input as immutable, and its output must not alias
// the input, since the commit outlives the pooled block result. It may read the store only to
// expand storage clears: other encoding overlaps the previous block's in-flight commit.
type NamedChangeSetEncoder func(StateChangeSet) ([]*proto.NamedChangeSet, error)

// BlockChangeSetEncoder contributes named changesets that are committed in the
// same CommitStateChanges call as the block's EVM state changes, so they are
// durable, rolled back and replayed together with that state. It is called
// after execution with the block's context and result, which it must treat as
// immutable. Changesets under keys.EVMStoreKey are reserved for the state encoder.
//
// As with NamedChangeSetEncoder, what it returns must not alias the result: the commit outlives
// the block.
type BlockChangeSetEncoder func(BlockContext, *BlockResult) ([]*proto.NamedChangeSet, error)

func (e *Executor) executePreparedBlockWithStore(ctx context.Context, req PreparedBlock) (*BlockResult, error) {
	stateStore := e.stateStore
	if stateStore == nil {
		return nil, errMissingStateStore
	}
	if e.changeSetEncoder == nil {
		return nil, errMissingNamedChangeSetEncoder
	}
	if req.Context.Number > maxGigaStoreBlockNumber {
		return nil, fmt.Errorf("giga store block number %d exceeds int64", req.Context.Number)
	}
	blockNumber := int64(req.Context.Number) //nolint:gosec // G115: bounded by maxGigaStoreBlockNumber above.

	// Store-backed execution is serialized so two blocks cannot share a stale
	// snapshot or overlap CommitStateChanges. Callers still submit block heights
	// in order.
	e.storeMu.Lock()
	defer e.storeMu.Unlock()
	// Closes the stage in flight, so the gap until the next block is charged to neither.
	defer e.blockPhases.Reset()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gigametrics.SetPhase(gigametrics.PhaseExecution)
	// Taken before the view, never after. Another goroutine may retire the previous commit at any
	// moment, and reading second would let it clear these changes after a view was opened that
	// predates them, leaving nothing to supply them. Read first, the worst case is a view that
	// already holds them and an overlay that replays the same values over the top.
	pending := e.pipelinePending()
	e.blockPhases.SetPhase(phaseOpenView)
	snapshot := stateStore.OpenView()
	if snapshot == nil {
		return nil, errors.New("giga store returned a nil snapshot")
	}
	defer snapshot.Close()

	// The previous block's commit may still be running, and this view cannot see it: a view "never
	// observes writes made after the view was opened". The overlay supplies exactly that block's
	// changes, so execution reads the state its predecessor produced without waiting for the write.
	var source StateReader = gigaSnapshotStateReader{
		snapshot:     snapshot,
		missingState: e.missingState,
	}
	source = newPendingOverlay(source, pending)

	e.blockPhases.SetPhase(phaseExecute)
	result, err := e.executePreparedBlock(ctx, req, source)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			result.Release()
		}
	}()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gigametrics.SetPhase(gigametrics.PhaseStorage)
	// The previous commit has to land before its changes leave the overlay. It has had this
	// block's whole execution to do that.
	e.blockPhases.SetPhase(phaseAwaitCommit)
	if err := e.awaitPipelineCommit(); err != nil {
		return nil, err
	}
	e.blockPhases.SetPhase(phaseAppHash)
	var extra []*proto.NamedChangeSet
	if e.blockChangeSetEncoder != nil {
		encoded, err := e.blockChangeSetEncoder(req.Context, result)
		if err != nil {
			return nil, fmt.Errorf("encode block changes for block %d: %w", req.Context.Number, err)
		}
		// An EVM-keyed changeset here would be written into account, storage and code
		// state as part of the block, diverging the app hash from the committed state.
		for _, cs := range encoded {
			if cs != nil && cs.Name == keys.EVMStoreKey {
				return nil, fmt.Errorf("block encoder returned a changeset named %q for block %d: %w",
					cs.Name, req.Context.Number, errBlockEncoderUsedEVMStoreKey)
			}
		}
		extra = encoded
	}
	if receiptStore := e.receiptStore; receiptStore != nil {
		e.blockPhases.SetPhase(phaseEncodeReceipts)
		records, err := e.receiptRecordsParallel(ctx, req.Context.Number, result)
		if err != nil {
			return nil, fmt.Errorf("encode receipts for block %d: %w", req.Context.Number, err)
		}
		e.blockPhases.SetPhase(phaseWriteReceipts)
		if err := receiptStore.SetReceipts(newReceiptContext(ctx, blockNumber), records); err != nil {
			return nil, fmt.Errorf("store receipts for block %d: %w", req.Context.Number, err)
		}
	}
	if err := e.startBackgroundCommit(ctx, blockNumber, result, extra); err != nil {
		return nil, fmt.Errorf("commit state changes for block %d: %w", req.Context.Number, err)
	}
	ok = true
	return result, nil
}

// AwaitCommits blocks until every block this executor has run is committed, and reports the first
// commit that failed.
//
// A block's commit runs behind the block that follows it, so the state a block produced is not in
// the store when ExecutePreparedBlock returns. A caller that reads the store directly — rather than
// through the next block's execution, which sees it regardless — has to wait here first. A run has
// to call it before reporting success, or a failed final commit goes unnoticed.
//
// A failure is remembered, so every later call reports it too, and it is safe to call from several
// goroutines at once.
func (e *Executor) AwaitCommits() error {
	if e == nil {
		return nil
	}
	return e.awaitPipelineCommit()
}

// pipelinePending returns the changes of a block whose commit has not been waited on yet, or nil
// when the store is caught up.
func (e *Executor) pipelinePending() *StateChangeSet {
	e.pipelineMu.Lock()
	defer e.pipelineMu.Unlock()
	return e.pipelineChanges
}

// awaitPipelineCommit blocks until the in-flight commit has landed, reporting the first commit that
// failed. After it returns the store holds every block this executor has run, so the next view
// opens on a known height and needs no overlay.
//
// The commit closes its channel rather than sending on it, so however many goroutines wait here
// they are all released.
func (e *Executor) awaitPipelineCommit() error {
	// Looped because a commit may have started while this waiter was blocked on the last one, and
	// the promise is that every block is committed when this returns, not merely the one in flight
	// when it was called.
	for {
		e.pipelineMu.Lock()
		done := e.pipelineDone
		e.pipelineMu.Unlock()
		if done == nil {
			break
		}
		<-done
		e.pipelineMu.Lock()
		// Only the waiters on this commit retire it; a later one owns its own state.
		var release func()
		if e.pipelineDone == done {
			if e.pipelineErr != nil && e.pipelineFailure == nil {
				e.pipelineFailure = fmt.Errorf("commit state changes: %w", e.pipelineErr)
			}
			e.pipelineDone = nil
			e.pipelineChanges = nil
			e.pipelineErr = nil
			release = e.pipelineRelease
			e.pipelineRelease = nil
		}
		e.pipelineMu.Unlock()
		if release != nil {
			release()
		}
	}
	e.pipelineMu.Lock()
	defer e.pipelineMu.Unlock()
	return e.pipelineFailure
}

// startBackgroundCommit publishes result's changes for the next block and commits them behind that
// block. Encoding the FlatKV pairs and the receipts runs in that background work. On a closed
// executor it commits synchronously instead.
//
// The previous commit has already been awaited. result stays readable until that next block finishes
// and this commit is retired.
func (e *Executor) startBackgroundCommit(ctx context.Context, blockNumber int64, result *BlockResult, extra []*proto.NamedChangeSet) error {
	// Close sets closed before taking storeMu, which the caller holds, so a block that sees it
	// unset is one Close waits for.
	if e.closed.Load() {
		changesets, err := e.encodeCommit(ctx, blockNumber, result, extra)
		if err != nil {
			return err
		}
		return e.commitState(blockNumber, changesets, &result.ChangeSet)
	}
	done := make(chan struct{})
	release := result.retain()
	e.pipelineMu.Lock()
	if failure := e.pipelineFailure; failure != nil {
		e.pipelineMu.Unlock()
		release()
		return failure
	}
	e.pipelineChanges = &result.ChangeSet
	e.pipelineRelease = release
	e.pipelineDone = done
	e.pipelineErr = nil
	e.pipelineMu.Unlock()

	go func() {
		// The caller returns before this finishes. Detach from its cancel so a shutdown of that
		// call does not drop a block whose app hash was already produced.
		err := e.finishBackgroundCommit(context.WithoutCancel(ctx), blockNumber, result, extra)
		e.pipelineMu.Lock()
		e.pipelineErr = err
		e.pipelineMu.Unlock()
		e.commitPhases.Reset()
		close(done)
	}()
	return nil
}

// finishBackgroundCommit encodes the block and writes it. It runs while the next block executes.
func (e *Executor) finishBackgroundCommit(ctx context.Context, blockNumber int64, result *BlockResult, extra []*proto.NamedChangeSet) error {
	changesets, err := e.encodeCommit(ctx, blockNumber, result, extra)
	if err != nil {
		return err
	}
	e.commitPhases.SetPhase(phaseCommitState)
	return e.commitState(blockNumber, changesets, &result.ChangeSet)
}

// encodeCommit builds the changesets and receipts the commit writes.
func (e *Executor) encodeCommit(ctx context.Context, blockNumber int64, result *BlockResult, extra []*proto.NamedChangeSet) ([]*proto.NamedChangeSet, error) {
	e.commitPhases.SetPhase(phaseEncodeChangesets)
	changesets, err := e.changeSetEncoder(result.ChangeSet)
	if err != nil {
		return nil, fmt.Errorf("encode state changes for block %d: %w", blockNumber, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append(changesets, extra...), nil
}

// commitState writes the block to the store, handing over changes as well when the store keeps the
// values the block replaced.
func (e *Executor) commitState(blockNumber int64, changesets []*proto.NamedChangeSet, changes *StateChangeSet) error {
	if store, ok := e.stateStore.(historyStore); ok {
		return store.CommitStateChangesWithPrior(blockNumber, changesets, changes)
	}
	return e.stateStore.CommitStateChanges(blockNumber, changesets)
}

type gigaSnapshotStateReader struct {
	snapshot     gigatypes.EVMStateView
	missingState StateReader
}

// A non-zero balance, nonce, or code proves the account exists in the snapshot,
// so the getters below only pay for the AccountExists probe when the field read
// back zero and a missingState fallback could change the answer.

func (r gigaSnapshotStateReader) GetBalance(addr common.Address) *big.Int {
	balance := r.snapshot.GetBalance(addr)
	if balance == (common.Hash{}) && r.useMissingState(addr) {
		return cloneBig(r.missingState.GetBalance(addr))
	}
	return new(big.Int).SetBytes(balance[:])
}

func (r gigaSnapshotStateReader) GetNonce(addr common.Address) uint64 {
	nonce := r.snapshot.GetNonce(addr)
	if nonce == 0 && r.useMissingState(addr) {
		return r.missingState.GetNonce(addr)
	}
	return nonce
}

func (r gigaSnapshotStateReader) GetCode(addr common.Address) []byte {
	code := r.snapshot.GetCode(addr)
	if len(code) == 0 && r.useMissingState(addr) {
		return cloneBytes(r.missingState.GetCode(addr))
	}
	return cloneBytes(code)
}

// ReadAccount returns addr's balance, nonce and code in one row read, and fetches code only for an
// account that has some. It declines an account the snapshot lacks when missingState must answer
// for it. Satisfies baseAccountReader.
func (r gigaSnapshotStateReader) ReadAccount(addr common.Address) (baseAccount, bool) {
	row, exists := r.snapshot.ReadAccount(addr)
	if !exists {
		return baseAccount{}, r.missingState == nil
	}
	account := baseAccount{
		Balance: new(big.Int).SetBytes(row.Balance[:]),
		Nonce:   row.Nonce,
	}
	// An account with the empty-code hash has no code, so the code store need not be asked.
	if row.CodeHash != gigatypes.EmptyCodeHash {
		account.Code = cloneBytes(r.snapshot.GetCode(addr))
	}
	return account, true
}

func (r gigaSnapshotStateReader) GetState(addr common.Address, key common.Hash) common.Hash {
	if r.useMissingState(addr) {
		return r.missingState.GetState(addr, key)
	}
	return r.snapshot.GetStorage(addr, key)
}

// useMissingState reports whether addr must be served from missingState: a
// fallback is configured and the snapshot holds no account for addr.
func (r gigaSnapshotStateReader) useMissingState(addr common.Address) bool {
	return r.missingState != nil && !r.snapshot.AccountExists(addr)
}

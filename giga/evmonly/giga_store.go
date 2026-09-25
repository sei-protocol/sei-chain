package evmonly

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	gigametrics "github.com/sei-protocol/sei-chain/giga/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

const maxGigaStoreBlockNumber = uint64(1<<63 - 1)

var (
	errMissingStateStore            = errors.New("executor requires a state store")
	errMissingNamedChangeSetEncoder = errors.New("giga store requires a named changeset encoder")
)

var _ StateReader = gigaSnapshotStateReader{}

// NamedChangeSetEncoder converts an executor-native state result into the on-disk changesets
// understood by a giga store. It must treat the input as immutable, and its output must not alias
// the input, since the commit outlives the pooled block result. It may read the store only to
// expand storage clears: other encoding overlaps the previous block's in-flight commit.
type NamedChangeSetEncoder func(StateChangeSet) ([]*proto.NamedChangeSet, error)

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
	e.blockPhases.SetPhase("open_view")
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

	e.blockPhases.SetPhase("execute")
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
	// Encoding that reads the store has to see a store holding every earlier block and none of
	// this one, so the previous commit lands first. Encoding that reads only this block's own
	// changes runs while that commit is still going, and waits below instead.
	settleBeforeEncoding := e.encodingReadsTheStore(&result.ChangeSet)
	if settleBeforeEncoding {
		e.blockPhases.SetPhase("await_commit")
		if err := e.awaitPipelineCommit(); err != nil {
			return nil, err
		}
	}
	e.blockPhases.SetPhase("encode_changesets")
	changesets, err := e.changeSetEncoder(result.ChangeSet)
	if err != nil {
		return nil, fmt.Errorf("encode state changes for block %d: %w", req.Context.Number, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if receiptStore := e.receiptStore; receiptStore != nil {
		e.blockPhases.SetPhase("encode_receipts")
		records, err := e.receiptRecordsParallel(ctx, req.Context.Number, result)
		if err != nil {
			return nil, fmt.Errorf("encode receipts for block %d: %w", req.Context.Number, err)
		}
		e.blockPhases.SetPhase("write_receipts")
		if err := receiptStore.SetReceipts(newReceiptContext(ctx, blockNumber), records); err != nil {
			return nil, fmt.Errorf("store receipts for block %d: %w", req.Context.Number, err)
		}
	}
	// One commit is in flight at a time, so the previous one lands before this block starts its
	// own. It has had this block's whole execution to run, so it rarely still holds.
	if !settleBeforeEncoding {
		e.blockPhases.SetPhase("await_commit")
		if err := e.awaitPipelineCommit(); err != nil {
			return nil, err
		}
	}
	e.blockPhases.SetPhase("commit_state")
	if err := e.startPipelineCommit(blockNumber, changesets, &result.ChangeSet); err != nil {
		return nil, fmt.Errorf("commit state changes for block %d: %w", req.Context.Number, err)
	}
	ok = true
	return result, nil
}

// encodingReadsTheStore reports whether encoding changes reads the live store, which a storage
// clear does to find the slots to delete. Such encoding must not overlap the previous block's commit.
func (e *Executor) encodingReadsTheStore(changes *StateChangeSet) bool {
	return len(changes.StorageClears) > 0
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
		if e.pipelineDone == done {
			if e.pipelineErr != nil && e.pipelineFailure == nil {
				e.pipelineFailure = fmt.Errorf("commit state changes: %w", e.pipelineErr)
			}
			e.pipelineDone = nil
			e.pipelineChanges = nil
			e.pipelineErr = nil
		}
		e.pipelineMu.Unlock()
	}
	e.pipelineMu.Lock()
	defer e.pipelineMu.Unlock()
	return e.pipelineFailure
}

// startPipelineCommit writes the block in the background and records what it changed, so the next
// block reads those changes through an overlay rather than waiting for the write. On a closed
// executor it commits synchronously instead.
//
// Commits stay ordered because only one is ever in flight: awaitPipelineCommit lands the previous
// one before this is called.
func (e *Executor) startPipelineCommit(blockNumber int64, changesets []*proto.NamedChangeSet, changes *StateChangeSet) error {
	// Close sets closed before taking storeMu, which the caller holds, so a block that sees it
	// unset is one Close waits for.
	if e.closed.Load() {
		if err := e.awaitPipelineCommit(); err != nil {
			return err
		}
		return e.stateStore.CommitStateChanges(blockNumber, changesets)
	}
	pending := changes.clone()
	done := make(chan struct{})
	e.pipelineMu.Lock()
	if failure := e.pipelineFailure; failure != nil {
		e.pipelineMu.Unlock()
		return failure
	}
	e.pipelineChanges = pending
	e.pipelineDone = done
	e.pipelineErr = nil
	e.pipelineMu.Unlock()

	go func() {
		err := e.stateStore.CommitStateChanges(blockNumber, changesets)
		e.pipelineMu.Lock()
		e.pipelineErr = err
		e.pipelineMu.Unlock()
		close(done)
	}()
	return nil
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

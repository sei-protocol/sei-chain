package evmonly

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	gigametrics "github.com/sei-protocol/sei-chain/giga/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

const maxGigaStoreBlockNumber = uint64(1<<63 - 1)

var (
	errMissingStateStore            = errors.New("executor requires a state store")
	errMissingReceiptStore          = errors.New("executor requires a receipt store")
	errMissingNamedChangeSetEncoder = errors.New("giga store requires a named changeset encoder")
	errBlockEncoderUsedEVMStoreKey  = errors.New("block changeset encoder may not write the EVM state changeset")
)

var _ StateReader = gigaSnapshotStateReader{}

// NamedChangeSetEncoder converts an executor-native state result into the
// on-disk changesets understood by a giga store. It is called in the background,
// after the previous block's commit has landed and before this block's starts, on a
// copy of the block's changes that it must treat as immutable.
//
// It may read the store, which then holds every earlier block and none of this one; expanding a
// storage clear does so.
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
	if e.receiptStore == nil {
		return nil, errMissingReceiptStore
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
	source = pending.overlay(source)

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
	// The receipts need nothing but the result, so their write starts here and runs under the rest
	// of the block's tail and the caller's, instead of after it.
	e.blockPhases.SetPhase("start_receipts")
	receipts := e.startReceiptWrite(ctx, blockNumber, result)
	// One commit is in flight at a time, so the previous one lands before this block starts its
	// own. It has had this block's whole execution to run, so it rarely still holds. A block encoder
	// that reads the store needs it landed before it runs; one that reads only the result overlaps
	// it. The state encoder runs in the background after this wait either way, so a storage clear
	// it expands against the store sees every earlier block and none of this one.
	settleBeforeBlockEncoder := e.blockChangeSetEncoder != nil && e.blockEncoderReadsStore
	if settleBeforeBlockEncoder {
		e.blockPhases.SetPhase("await_commit")
		if err := e.awaitPipelineCommit(); err != nil {
			return nil, err
		}
	}
	// The block encoder stays on the loop: what it stages is what the caller reports for the block,
	// so it has to have run when this returns.
	var extra []*proto.NamedChangeSet
	if e.blockChangeSetEncoder != nil {
		e.blockPhases.SetPhase("encode_block_changesets")
		extra, err = e.blockChangeSetEncoder(req.Context, result)
		if err != nil {
			return nil, fmt.Errorf("encode block changes for block %d: %w", req.Context.Number, err)
		}
		// An EVM-keyed changeset here would be written into account, storage and code
		// state as part of the block, diverging the app hash from the committed state.
		for _, cs := range extra {
			if cs != nil && cs.Name == keys.EVMStoreKey {
				return nil, fmt.Errorf("block encoder returned a changeset named %q for block %d: %w",
					cs.Name, req.Context.Number, errBlockEncoderUsedEVMStoreKey)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !settleBeforeBlockEncoder {
		e.blockPhases.SetPhase("await_commit")
		if err := e.awaitPipelineCommit(); err != nil {
			return nil, err
		}
	}
	e.blockPhases.SetPhase("start_commit")
	if err := e.startPipelineCommit(blockNumber, result, receipts, extra); err != nil {
		return nil, fmt.Errorf("commit state changes for block %d: %w", req.Context.Number, err)
	}
	ok = true
	return result, nil
}

// receiptWrite is one block's background receipt write: done is closed once err is set.
type receiptWrite struct {
	done chan struct{}
	err  error
}

// AwaitReceipts blocks until the receipts of the last block this executor ran are in the receipt
// store, and reports the write's failure if it had one.
//
// Receipts are written in the background behind ExecutePreparedBlock. A caller that publishes a
// block to readers who expect its receipts has to wait here first. Unlike AwaitCommits it does not
// wait for the block's state commit, which keeps landing behind the next block.
func (e *Executor) AwaitReceipts() error {
	if e == nil {
		return nil
	}
	e.pipelineMu.Lock()
	write := e.pipelineReceipts
	e.pipelineMu.Unlock()
	if write == nil {
		return nil
	}
	<-write.done
	return write.err
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
func (e *Executor) pipelinePending() *pendingChanges {
	e.pipelineMu.Lock()
	defer e.pipelineMu.Unlock()
	return e.pipelineChanges
}

// LatestAccount is the balance and nonce of an account after the last block this executor ran.
type LatestAccount struct {
	Balance *big.Int
	Nonce   uint64
}

// ReadLatestAccount returns addr's balance and nonce after the last block this executor ran,
// without waiting for that block's commit to land. It reports the first failed commit instead of
// state that lacks the failed block.
func (e *Executor) ReadLatestAccount(addr common.Address) (LatestAccount, error) {
	if e.stateStore == nil {
		return LatestAccount{}, errMissingStateStore
	}
	for {
		e.pipelineMu.Lock()
		pending, generation, failure := e.pipelineChanges, e.pipelineGeneration, e.pipelineFailureLocked()
		e.pipelineMu.Unlock()
		if failure != nil {
			return LatestAccount{}, failure
		}
		snapshot := e.stateStore.OpenView()
		if snapshot == nil {
			return LatestAccount{}, errors.New("giga store returned a nil snapshot")
		}
		account, ok := e.readLatestAccount(snapshot, pending, generation, addr)
		snapshot.Close()
		if ok {
			return account, nil
		}
	}
}

// readLatestAccount reads addr through pending laid over snapshot. It reports false when another
// commit started after generation was read, since the view may then hold a later block's writes and
// pending would replay older values over them; the caller reads again.
func (e *Executor) readLatestAccount(snapshot gigatypes.EVMStateView, pending *pendingChanges, generation uint64, addr common.Address) (LatestAccount, bool) {
	e.pipelineMu.Lock()
	moved := e.pipelineGeneration != generation
	e.pipelineMu.Unlock()
	if moved {
		return LatestAccount{}, false
	}
	reader := pending.overlay(gigaSnapshotStateReader{snapshot: snapshot, missingState: e.missingState})
	if rowReader, ok := reader.(accountSnapshotReader); ok {
		if row, ok := rowReader.ReadAccount(addr); ok {
			balance := row.Balance
			if balance == nil {
				balance = new(big.Int)
			}
			return LatestAccount{Balance: balance, Nonce: row.Nonce}, true
		}
	}
	return LatestAccount{Balance: reader.GetBalance(addr), Nonce: reader.GetNonce(addr)}, true
}

// pipelineFailureLocked returns the first failed commit, whether or not a waiter has retired it yet.
// Callers hold pipelineMu.
func (e *Executor) pipelineFailureLocked() error {
	if e.pipelineFailure != nil {
		return e.pipelineFailure
	}
	if e.pipelineErr != nil {
		return fmt.Errorf("persist block: %w", e.pipelineErr)
	}
	return nil
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
				e.pipelineFailure = fmt.Errorf("persist block: %w", e.pipelineErr)
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

// startReceiptWrite persists the block's receipts in the background, after the previous block's
// have landed, and returns the write to wait on. The block result is held until the write has
// landed.
//
// The write is recorded as the executor's newest, so AwaitReceipts finds it whether or not the
// block's commit is started afterwards.
func (e *Executor) startReceiptWrite(ctx context.Context, blockNumber int64, result *BlockResult) *receiptWrite {
	receipts := &receiptWrite{done: make(chan struct{})}
	e.pipelineMu.Lock()
	previous := e.pipelineReceipts
	e.pipelineReceipts = receipts
	e.pipelineMu.Unlock()

	// The write finishes even if the request that ran the block is cancelled: Close waits for it,
	// and a failure is reported through the pipeline rather than by dropping the block.
	bgCtx := context.WithoutCancel(ctx)
	releaseResult := result.retain()
	go func() {
		defer close(receipts.done)
		defer releaseResult()
		// Receipts land in block order, so the store's version means every block up to it. The
		// previous write owns the phase timer until it is done.
		if previous != nil {
			<-previous.done
			if previous.err != nil {
				receipts.err = fmt.Errorf("receipts for block %d not written after an earlier failure: %w", blockNumber, previous.err)
				return
			}
		}
		defer e.receiptPhases.Reset()
		receipts.err = e.persistReceipts(bgCtx, blockNumber, result)
	}()
	return receipts
}

// startPipelineCommit commits the block's encoded state changes in the background once its
// receipts have landed, and records what it changed, so the next block reads those changes through
// an overlay rather than waiting for the write. The state changes are encoded from a copy that
// outlives the block result.
//
// Commits stay ordered because only one is ever in flight: awaitPipelineCommit lands the previous
// one before this is called.
func (e *Executor) startPipelineCommit(blockNumber int64, result *BlockResult, receipts *receiptWrite, extra []*proto.NamedChangeSet) error {
	changes := result.ChangeSet.clone()
	pending := newPendingChanges(changes)
	done := make(chan struct{})
	e.pipelineMu.Lock()
	if failure := e.pipelineFailure; failure != nil {
		e.pipelineMu.Unlock()
		return failure
	}
	e.pipelineChanges = pending
	e.pipelineGeneration++
	e.pipelineDone = done
	e.pipelineErr = nil
	e.pipelineMu.Unlock()

	go func() {
		defer close(done)
		defer e.pipelinePhases.Reset()
		// A block whose receipts were lost is a failed block: its state is not committed, so the
		// store never holds a block whose receipts cannot be read.
		e.pipelinePhases.SetPhase("await_receipt_write")
		<-receipts.done
		if receipts.err != nil {
			e.pipelineMu.Lock()
			e.pipelineErr = receipts.err
			e.pipelineMu.Unlock()
			return
		}
		err := e.commitStateChanges(blockNumber, changes, extra)
		e.pipelineMu.Lock()
		e.pipelineErr = err
		e.pipelineMu.Unlock()
	}()
	return nil
}

// persistReceipts encodes the block's receipts and writes them to the receipt store, returning once
// they are readable there.
func (e *Executor) persistReceipts(ctx context.Context, blockNumber int64, result *BlockResult) error {
	e.receiptPhases.SetPhase("encode_receipts")
	records, err := e.receiptRecordsParallel(ctx, uint64(blockNumber), result) //nolint:gosec // G115: non-negative, checked by the caller.
	if err != nil {
		return fmt.Errorf("encode receipts for block %d: %w", blockNumber, err)
	}
	e.receiptPhases.SetPhase("write_receipts")
	if err := e.receiptStore.SetReceipts(newReceiptContext(ctx, blockNumber), records); err != nil {
		return fmt.Errorf("store receipts for block %d: %w", blockNumber, err)
	}
	// A store that applies writes from its own queue reports the landing through its version;
	// a write it dropped after accepting shows up as a version short of this block.
	if waiter, ok := e.receiptStore.(seidbtypes.PendingWriteWaiter); ok {
		e.receiptPhases.SetPhase("await_store")
		waiter.WaitForPendingWrites()
		if latest := e.receiptStore.LatestVersion(); latest < blockNumber {
			return fmt.Errorf("receipts for block %d did not land: receipt store is at block %d", blockNumber, latest)
		}
	}
	return nil
}

// commitStateChanges encodes the block's state changes, appends the block encoder's, and commits
// them to the state store.
func (e *Executor) commitStateChanges(blockNumber int64, changes *StateChangeSet, extra []*proto.NamedChangeSet) error {
	e.pipelinePhases.SetPhase("encode_changesets")
	var stateChanges StateChangeSet
	if changes != nil {
		stateChanges = *changes
	}
	changesets, err := e.changeSetEncoder(stateChanges)
	if err != nil {
		return fmt.Errorf("encode state changes for block %d: %w", blockNumber, err)
	}
	changesets = append(changesets, extra...)
	e.pipelinePhases.SetPhase("commit_state")
	return e.stateStore.CommitStateChanges(blockNumber, changesets)
}

type gigaSnapshotStateReader struct {
	snapshot     gigatypes.EVMStateView
	missingState StateReader
}

func (r gigaSnapshotStateReader) GetBalance(addr common.Address) *big.Int {
	if r.missingState != nil && !r.snapshot.AccountExists(addr) {
		return cloneBig(r.missingState.GetBalance(addr))
	}
	balance := r.snapshot.GetBalance(addr)
	return new(big.Int).SetBytes(balance[:])
}

func (r gigaSnapshotStateReader) GetNonce(addr common.Address) uint64 {
	if r.missingState != nil && !r.snapshot.AccountExists(addr) {
		return r.missingState.GetNonce(addr)
	}
	return r.snapshot.GetNonce(addr)
}

func (r gigaSnapshotStateReader) GetCode(addr common.Address) []byte {
	if r.missingState != nil && !r.snapshot.AccountExists(addr) {
		return cloneBytes(r.missingState.GetCode(addr))
	}
	return cloneBytes(r.snapshot.GetCode(addr))
}

// ReadAccount returns addr's balance, nonce and code in one row read, and fetches code only for an
// account that has some. Satisfies accountSnapshotReader.
func (r gigaSnapshotStateReader) ReadAccount(addr common.Address) (accountSnapshot, bool) {
	reader, ok := r.snapshot.(gigatypes.AccountReader)
	if !ok {
		return accountSnapshot{}, false
	}
	if r.missingState != nil && !r.snapshot.AccountExists(addr) {
		return accountSnapshot{}, false
	}
	row, exists := reader.ReadAccount(addr)
	if !exists {
		return accountSnapshot{}, true
	}
	snapshot := accountSnapshot{
		Balance: new(big.Int).SetBytes(row.Balance[:]),
		Nonce:   row.Nonce,
	}
	// An account with the empty-code hash has no code, so the code store need not be asked.
	if row.CodeHash != gigatypes.EmptyCodeHash {
		snapshot.Code = cloneBytes(r.snapshot.GetCode(addr))
	}
	return snapshot, true
}

func (r gigaSnapshotStateReader) GetState(addr common.Address, key common.Hash) common.Hash {
	if r.missingState != nil && !r.snapshot.AccountExists(addr) {
		return r.missingState.GetState(addr, key)
	}
	return r.snapshot.GetStorage(addr, key)
}

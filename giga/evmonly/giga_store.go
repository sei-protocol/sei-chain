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
	errMissingReceiptStore          = errors.New("executor requires a receipt store")
	errMissingNamedChangeSetEncoder = errors.New("giga store requires a named changeset encoder")
	errBlockEncoderUsedEVMStoreKey  = errors.New("block changeset encoder may not write the EVM state changeset")
)

var _ StateReader = gigaSnapshotStateReader{}

// NamedChangeSetEncoder converts an executor-native state result into the
// on-disk changesets understood by a giga store. It is called synchronously
// while the block's read snapshot is still open. It must treat the input as
// immutable and must not retain references to it after returning.
//
// What it returns must not alias the input either. The commit runs in the background, outliving
// the block result and its return to the pool, so an aliasing pair would be rewritten underneath
// the write by the next block.
//
// It must read only the changeset it is given. Encoding overlaps the previous block's commit, so
// an encoder that reads the store would see a store mid-write. Expanding a storage clear is the
// one exception, and the executor waits for that commit before encoding a block that has one.
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
	receiptStore := e.receiptStore
	if receiptStore == nil {
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
	if e.blockChangeSetEncoder != nil {
		extra, err := e.blockChangeSetEncoder(req.Context, result)
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
		changesets = append(changesets, extra...)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.blockPhases.SetPhase("encode_receipts")
	records, err := e.receiptRecordsParallel(ctx, req.Context.Number, result)
	if err != nil {
		return nil, fmt.Errorf("encode receipts for block %d: %w", req.Context.Number, err)
	}
	e.blockPhases.SetPhase("write_receipts")
	if err := receiptStore.SetReceipts(newReceiptContext(ctx, blockNumber), records); err != nil {
		return nil, fmt.Errorf("store receipts for block %d: %w", req.Context.Number, err)
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

// encodingReadsTheStore reports whether encoding this block's changesets reads the store as well as
// the block's own changes, which decides whether encoding may overlap the previous block's commit.
//
// Expanding a storage clear iterates the live store to find the slots to delete, so a block that
// clears one must not be encoded against a store mid-commit. A block encoder is caller-supplied and
// free to read whatever it likes, so one is assumed to read the store unless it was registered as
// store-independent: assuming otherwise would surrender the receipt-stage slack on every block.
func (e *Executor) encodingReadsTheStore(changes *StateChangeSet) bool {
	return len(changes.StorageClears) > 0 || (e.blockChangeSetEncoder != nil && e.blockEncoderReadsStore)
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
		return fmt.Errorf("commit state changes: %w", e.pipelineErr)
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
// block reads those changes through an overlay rather than waiting for the write.
//
// Commits stay ordered because only one is ever in flight: awaitPipelineCommit lands the previous
// one before this is called.
func (e *Executor) startPipelineCommit(blockNumber int64, changesets []*proto.NamedChangeSet, changes *StateChangeSet) error {
	pending := newPendingChanges(changes.clone())
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

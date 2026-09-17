package evmonly

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	gigametrics "github.com/sei-protocol/sei-chain/giga/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

const maxGigaStoreBlockNumber = uint64(1<<63 - 1)

var (
	errMissingStateStore            = errors.New("executor requires a state store")
	errMissingReceiptStore          = errors.New("executor requires a receipt store")
	errMissingNamedChangeSetEncoder = errors.New("giga store requires a named changeset encoder")
)

var _ StateReader = gigaSnapshotStateReader{}

// NamedChangeSetEncoder converts an executor-native state result into the
// on-disk changesets understood by a giga store. It is called synchronously
// while the block's read snapshot is still open. It must treat the input as
// immutable and must not retain references to it after returning.
type NamedChangeSetEncoder func(StateChangeSet) ([]*proto.NamedChangeSet, error)

// BlockChangeSetEncoder contributes named changesets that are committed in the
// same CommitStateChanges call as the block's EVM state changes, so they are
// durable, rolled back and replayed together with that state. It is called
// after execution with the block's context and result, which it must treat as
// immutable. Changesets under keys.EVMStoreKey are reserved for the state encoder.
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
	source = newPendingOverlay(source, e.pipelinePending())

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
	// Waited for here rather than before execution: the previous block's commit has had this whole
	// block to run, so by now it has almost always finished. Landing it before the next OpenView is
	// what keeps that view's height unambiguous.
	e.blockPhases.SetPhase("await_commit")
	if err := e.awaitPipelineCommit(); err != nil {
		return nil, err
	}
	e.blockPhases.SetPhase("commit_state")
	if err := e.startPipelineCommit(blockNumber, changesets, &result.ChangeSet); err != nil {
		return nil, fmt.Errorf("commit state changes for block %d: %w", req.Context.Number, err)
	}
	ok = true
	return result, nil
}

// AwaitCommits blocks until every block this executor has run is committed, and reports the first
// failure among them.
//
// A block's commit runs behind the block that follows it, so the state a block produced is not in
// the store when ExecutePreparedBlock returns. A caller that reads the store directly — rather than
// through the next block's execution, which sees it regardless — has to wait here first.
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

// awaitPipelineCommit blocks until the in-flight commit has landed, reporting its failure. After it
// returns the store holds every block this executor has run, so the next view opens on a known
// height and needs no overlay.
func (e *Executor) awaitPipelineCommit() error {
	e.pipelineMu.Lock()
	done := e.pipelineDone
	e.pipelineMu.Unlock()
	if done == nil {
		return nil
	}
	err := <-done
	e.pipelineMu.Lock()
	e.pipelineDone = nil
	e.pipelineChanges = nil
	e.pipelineMu.Unlock()
	if err != nil {
		return fmt.Errorf("commit state changes: %w", err)
	}
	return nil
}

// startPipelineCommit writes the block in the background and records what it changed, so the next
// block reads those changes through an overlay rather than waiting for the write.
//
// Commits stay ordered because only one is ever in flight: awaitPipelineCommit lands the previous
// one before this is called.
func (e *Executor) startPipelineCommit(blockNumber int64, changesets []*proto.NamedChangeSet, changes *StateChangeSet) error {
	pending := changes.clone()
	done := make(chan error, 1)
	e.pipelineMu.Lock()
	e.pipelineChanges = pending
	e.pipelineDone = done
	e.pipelineMu.Unlock()

	go func() {
		done <- e.stateStore.CommitStateChanges(blockNumber, changesets)
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
	if common.Hash(row.CodeHash) != types.EmptyCodeHash {
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

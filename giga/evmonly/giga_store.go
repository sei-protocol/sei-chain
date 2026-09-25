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
	errMissingReceiptStore          = errors.New("executor requires a receipt store")
	errMissingNamedChangeSetEncoder = errors.New("giga store requires a named changeset encoder")
)

var _ StateReader = gigaSnapshotStateReader{}

// placeholderBlockHashRetention is how many of the newest blocks keep their state hashes. It is a
// placeholder until a real threshold is wired in.
const placeholderBlockHashRetention = 10_000

// NamedChangeSetEncoder converts an executor-native state result into the
// on-disk changesets understood by a giga store. It is called synchronously
// while the block's read snapshot is still open. It must treat the input as
// immutable and must not retain references to it after returning.
type NamedChangeSetEncoder func(StateChangeSet) ([]*proto.NamedChangeSet, error)

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

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gigametrics.SetPhase(gigametrics.PhaseExecution)
	snapshot := stateStore.OpenView()
	if snapshot == nil {
		return nil, errors.New("giga store returned a nil snapshot")
	}
	defer snapshot.Close()

	result, err := e.executePreparedBlock(ctx, req, gigaSnapshotStateReader{
		snapshot:     snapshot,
		missingState: e.missingState,
	})
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
	changesets, err := e.changeSetEncoder(result.ChangeSet)
	if err != nil {
		return nil, fmt.Errorf("encode state changes for block %d: %w", req.Context.Number, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records, err := receiptRecords(req.Context.Number, result)
	if err != nil {
		return nil, fmt.Errorf("encode receipts for block %d: %w", req.Context.Number, err)
	}
	if err := receiptStore.SetReceipts(newReceiptContext(ctx, blockNumber), records); err != nil {
		return nil, fmt.Errorf("store receipts for block %d: %w", req.Context.Number, err)
	}
	if err := stateStore.CommitStateChanges(blockNumber, changesets); err != nil {
		return nil, fmt.Errorf("commit state changes for block %d: %w", req.Context.Number, err)
	}
	// PLACEHOLDER: keeps the newest placeholderBlockHashRetention blocks' hashes. A real threshold, set by
	// what giga execution needs block hashes for, should be wired in here.
	if err := stateStore.PruneBlockHashesBelow(blockHashesPrunedBelow(req.Context.Number)); err != nil {
		return nil, fmt.Errorf("prune block hashes after block %d: %w", req.Context.Number, err)
	}
	ok = true
	return result, nil
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

// blockHashesPrunedBelow returns the block below which state hashes may be pruned once blockNumber is
// committed.
func blockHashesPrunedBelow(blockNumber uint64) uint64 {
	if blockNumber < placeholderBlockHashRetention {
		return 0
	}
	return blockNumber - placeholderBlockHashRetention
}

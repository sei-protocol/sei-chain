package evmonly

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigastore "github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
)

const maxGigaStoreBlockNumber = uint64(1<<63 - 1)

var (
	errMissingStorageManager        = errors.New("executor requires a storage manager")
	errMissingStateStore            = errors.New("storage manager requires a state store")
	errMissingReceiptStore          = errors.New("storage manager requires a receipt store")
	errMissingNamedChangeSetEncoder = errors.New("giga store requires a named changeset encoder")
)

var _ StateReader = gigaSnapshotStateReader{}

// NamedChangeSetEncoder converts an executor-native state result into the
// on-disk changesets understood by a giga store. It is called synchronously
// while the block's read snapshot is still open. It must treat the input as
// immutable and must not retain references to it after returning.
type NamedChangeSetEncoder func(StateChangeSet) ([]*proto.NamedChangeSet, error)

func (e *Executor) executePreparedBlockWithStore(ctx context.Context, req PreparedBlock) (*BlockResult, error) {
	if e.storageManager == nil {
		return nil, errMissingStorageManager
	}
	stateStore := e.storageManager.StateDB()
	if stateStore == nil {
		return nil, errMissingStateStore
	}
	receiptStore := e.storageManager.ReceiptDB()
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
	snapshot := stateStore.OpenView()
	if snapshot == nil {
		return nil, errors.New("giga store returned a nil snapshot")
	}
	defer snapshot.Close()

	result, err := e.executePreparedBlock(ctx, req, gigaSnapshotStateReader{snapshot: snapshot})
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
	if err := stateStore.CommitStateChanges(blockNumber, changesets); err != nil {
		return nil, fmt.Errorf("commit state changes for block %d: %w", req.Context.Number, err)
	}
	if err := receiptStore.SetReceipts(newReceiptContext(ctx, blockNumber), records); err != nil {
		return nil, fmt.Errorf("store receipts for block %d: %w", req.Context.Number, err)
	}
	ok = true
	return result, nil
}

type gigaSnapshotStateReader struct {
	snapshot gigastore.EVMStateView
}

func (r gigaSnapshotStateReader) GetBalance(addr common.Address) *big.Int {
	balance := r.snapshot.GetBalance(addr)
	return new(big.Int).SetBytes(balance[:])
}

func (r gigaSnapshotStateReader) GetNonce(addr common.Address) uint64 {
	return r.snapshot.GetNonce(addr)
}

func (r gigaSnapshotStateReader) GetCode(addr common.Address) []byte {
	return cloneBytes(r.snapshot.GetCode(addr))
}

func (r gigaSnapshotStateReader) GetState(addr common.Address, key common.Hash) common.Hash {
	return r.snapshot.GetStorage(addr, key)
}

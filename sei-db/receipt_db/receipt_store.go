// Package receiptdb defines the receipt store interface that every receipt backend implements.
package receiptdb

import (
	"errors"
	"fmt"

	receipttypes "github.com/sei-protocol/sei-chain/sei-db/receipt_db/types"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"
	"github.com/sei-protocol/sei-chain/sei-db/receipt_db/logfilter"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// Sentinel errors for consistent error checking.
var (
	ErrNotFound               = errors.New("receipt not found")
	ErrNotConfigured          = errors.New("receipt store not configured")
	ErrRangeQueryNotSupported = errors.New("range query not supported by this backend")
	// ErrBlockStatsNotSupported is returned by GetBlockStats when the backend never recorded
	// block stats for that block; the caller falls back to summing its receipts.
	ErrBlockStatsNotSupported = errors.New("block stats not supported")
)

// ReceiptStore exposes receipt-specific operations without leaking the StateStore interface.
type ReceiptStore interface {
	controllertypes.PrunableStore

	// LatestVersion is the highest block whose receipts are queryable. A write may land after
	// SetReceipts returns, so a reader follows this rather than the height it last wrote.
	LatestVersion() int64

	EarliestVersion() int64

	GetReceipt(ctx sdk.Context, txHash common.Hash) (*evmtypes.Receipt, error)

	GetReceiptFromStore(ctx sdk.Context, txHash common.Hash) (*evmtypes.Receipt, error)

	// SetReceipts writes the block's receipts, carrying the version markers with them. An
	// implementation may apply the write in the background; LatestVersion reports when it lands.
	//
	// Every block must be written, one that produced no receipts included; an implementation may
	// refuse a write that skips a block.
	SetReceipts(ctx sdk.Context, receipts []receipttypes.ReceiptRecord) error

	// Flush returns once every write made by a SetReceipts() call that returned before it has landed
	// and is reflected in LatestVersion().
	Flush() error

	// GetBlockStats returns the aggregate stats recorded when the block's receipts were written.
	// See ErrNotFound and ErrBlockStatsNotSupported.
	GetBlockStats(ctx sdk.Context, blockNumber uint64) (receipttypes.BlockStats, error)

	// FilterLogs queries logs across a range of blocks.
	// For single-block queries, set fromBlock == toBlock.
	// budget is charged per matched log via Reserve and aborts once either
	// configured ceiling is exceeded; nil disables all caps. Callers on the
	// eth range path typically pass a byte-only budget (maxLog=0) here and
	// enforce the matched-log count on the normalized result separately.
	//
	// A store that cannot answer a range query returns ErrRangeQueryNotSupported.
	FilterLogs(
		ctx sdk.Context,
		fromBlock uint64,
		toBlock uint64,
		crit filters.FilterCriteria,
		budget *logfilter.LogBudget,
	) ([]*ethtypes.Log, error)

	// IterateReceipts walks every receipt stored at or above startBlock, in ascending block
	// order and by transaction index within a block. A startBlock below the oldest receipt the
	// store holds begins at the oldest receipt it holds.
	//
	// A backend that cannot walk its receipts returns
	// ErrRangeQueryNotSupported.
	IterateReceipts(startBlock uint64) (ReceiptIterator, error)

	Close() error
}

// ReceiptIterator walks stored receipts. It is not safe for concurrent use.
type ReceiptIterator interface {
	// Next advances to the next receipt, reporting false once the walk is complete. After it
	// returns false or an error, only Close may be called.
	Next() (bool, error)

	// BlockNumber returns the block holding the current receipt. Valid only after Next
	// returned true.
	BlockNumber() uint64

	// TxHash returns the hash of the current receipt's transaction. Valid only after Next
	// returned true.
	TxHash() common.Hash

	// Receipt decodes the current receipt. Valid only after Next returned true.
	Receipt() (*evmtypes.Receipt, error)

	// Close releases the iterator's resources. It MUST be called; failing to do so pins
	// segment files on disk.
	Close() error
}

// VersionPinner is implemented by receipt stores whose version markers can be written directly. It
// is for a caller that put receipts in place by other means and has to state the window they cover.
type VersionPinner interface {
	SetLatestVersion(version int64) error
	SetEarliestVersion(version int64) error
}

// PinVersions widens store's queryable window to [earliest, latest], reporting a store that cannot
// be pinned rather than leaving the window unset.
func PinVersions(store ReceiptStore, earliest, latest int64) error {
	pinner, ok := store.(VersionPinner)
	if !ok {
		return fmt.Errorf("receipt store %T cannot pin versions", store)
	}
	if err := pinner.SetLatestVersion(latest); err != nil {
		return err
	}
	return pinner.SetEarliestVersion(earliest)
}

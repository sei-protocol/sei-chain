package types

import (
	"errors"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// ErrBlockOutOfOrder is returned by WriteBlock when the supplied
// GlobalBlockNumber is not strictly greater than every previously written
// block number. Blocks must be written in strictly ascending order.
var ErrBlockOutOfOrder = errors.New("block: WriteBlock out of order")

// ErrQCNonContiguous is returned by WriteQC when the QC's GlobalRange().First
// does not equal the previous QC's GlobalRange().Next. QCs must be written as
// a contiguous, ascending sequence.
var ErrQCNonContiguous = errors.New("block: WriteQC non-contiguous")

// ErrBlockMissingQC is returned by WriteBlock when no previously written QC
// covers the block's GlobalBlockNumber. A QC covering a block must be written
// before that block (see the BlockDB ordering contract).
var ErrBlockMissingQC = errors.New("block: WriteBlock without covering QC")

// ErrPruned is returned by the by-number read methods (ReadBlockByNumber and
// ReadQCByBlockNumber) when the requested GlobalBlockNumber is strictly below
// the current retention watermark: the record is treated as pruned and is not
// served while below the watermark. It is distinct from a utils.None result,
// which means "not present at or above the watermark" and may still be filled
// by a future write.
//
// ErrPruned reflects the watermark's current position, not a permanent verdict.
// The watermark only advances while a store stays open, so within a single
// session ErrPruned is terminal — retrying the same n keeps returning it. It is
// not durable across restarts: the watermark is re-derived on open and
// reclamation is asynchronous, so an n that returned ErrPruned before a restart
// may afterward read as present (or as utils.None). Callers should treat
// ErrPruned as "not currently served," not as a guarantee the record is gone.
var ErrPruned = errors.New("block: below retention watermark")

// BlockDB is the durable backing store for data.State. It persists the two
// kinds of finalized records the consensus state machine produces —
// finalized blocks (indexed by GlobalBlockNumber and by header hash) and
// FullCommitQCs (each covering a contiguous range of GlobalBlockNumbers) —
// and provides the read API needed for crash recovery and runtime lookups.
//
// # Concurrency
//
// All methods are safe for concurrent use. Implementations should expect
// concurrent writes (WriteBlock + WriteQC interleaved from a single
// background persistence loop) and concurrent reads from RPC handlers
// and peer-sync streams.
//
// # Durability and crash safety
//
// Writes are two-phase: WriteBlock and WriteQC return without
// guaranteeing the record is on disk. Flush blocks until all
// previously-returned Writes are durable.
//
// Read-your-writes is provided within a single session regardless of
// Flush — a Write followed by a Read in the same process always
// observes the Write. Flush is about disk durability, not in-process
// visibility.
//
// # Ordering and the GlobalRange convention
//
// A FullCommitQC's GlobalRange is a half-open interval
// [GlobalRange.First(), GlobalRange.Next()) — First inclusive, Next
// exclusive. The QC therefore covers GlobalBlockNumbers First, First+1,
// ..., Next-1, and Next is also the First of the next contiguous QC.
//
// Writes must be ordered, and the contract is enforced (not merely
// expected):
//
//   - Blocks must be written in strictly ascending GlobalBlockNumber
//     order. WriteBlock returns ErrBlockOutOfOrder otherwise.
//   - QCs must be written contiguously — each WriteQC's lowerBound
//     must equal the previous WriteQC's upperBound. WriteQC returns
//     ErrQCNonContiguous otherwise.
//   - QCs must be written before blocks. A QC covering a block must
//     be written before that block is written.
//
// After a crash, data not flushed may be lost, but the following invariants hold:
//
//   - Individual blocks and QCs are either fully persisted or not at all; there are no partial writes.
//   - Data is persisted in order, meaning that data loss never leaves gaps. If A is written and then B
//     is written, then after a crash if B is persisted then A is also persisted.
//   - Since QCs must always be written before the blocks they cover, a persisted block is always covered
//     by a persisted QC, but a persisted QC may or may not have its covered blocks persisted.
//
// # A readable block always has a readable covering QC
//
// Pruning never leaves a block readable without its covering QC also being readable. And if a block becomes
// crash recoverable, its QC is guaranteed to also be crash recoverable.
type BlockDB interface {
	// WriteBlock persists a finalized block at GlobalBlockNumber n. A
	// block for height n may only be written after a QC covering n has
	// also been written, or else this method returns an error.
	//
	// n must be strictly greater than every previously written block
	// number; otherwise WriteBlock returns ErrBlockOutOfOrder and
	// persists nothing. Writes are NOT idempotent — re-writing the same
	// (or any non-ascending) n is rejected with an error.
	//
	// May return before the block is on disk. Callers that need crash
	// durability before some external observable action (e.g.
	// runPersist advancing nextBlockToPersist, which gates the
	// AppVote runExecute issues) must call Flush. See the BlockDB type
	// doc for the two-phase write/flush contract.
	//
	// Writes are made crash durable in write order (both blocks and QCs),
	// so loss of non-durable data after a crash never leaves gaps.
	WriteBlock(n GlobalBlockNumber, block *Block) error

	// WriteQC persists a FullCommitQC covering the half-open global block
	// number range [lowerBound, upperBound) — lowerBound inclusive,
	// upperBound exclusive (i.e. the QC finalizes lowerBound, lowerBound+1,
	// ..., upperBound-1).
	//
	// Successive WriteQC calls must form a contiguous sequence: each
	// call's lowerBound must equal the previous call's upperBound (the
	// first QC may start anywhere). A gap or overlap returns
	// ErrQCNonContiguous and persists nothing. Writes are NOT idempotent —
	// re-writing a QC is rejected rather than treated as a no-op.
	//
	// May return before the QC is on disk. See the BlockDB type doc for
	// the two-phase write/flush contract and WriteBlock for the
	// rationale.
	//
	// Writes are made crash durable in write order (both blocks and QCs),
	// so loss of non-durable data after a crash never leaves gaps.
	WriteQC(
		lowerBound GlobalBlockNumber,
		upperBound GlobalBlockNumber,
		qc *FullCommitQC,
	) error

	// PruneBefore advances the retention watermark toward n and removes
	// everything below it:
	//   - every block with GlobalBlockNumber < watermark
	//   - every QC whose GlobalRange().Next ≤ watermark (its entire
	//     covered range is below the watermark; a QC straddling the
	//     watermark stays)
	//
	// A QC's cohort of blocks changes readability atomically: the watermark
	// never falls strictly inside a QC's covered range. A requested n that
	// lands inside a QC's range is rounded DOWN to that QC's GlobalRange().First,
	// so the whole cohort stays readable until a later prune reaches the QC's
	// Next. (Rounding down, not up, because blocks at or above n must be
	// retained.) The watermark is therefore always a QC boundary.
	//
	// Idempotent: calling with n ≤ the existing retention watermark is
	// a no-op; the watermark only advances.
	//
	// Pruning never empties the store. Once a block has been written, at
	// least one block (and a QC covering it) always remains readable — a
	// request that would remove every block is capped to retain the most
	// recently written block (and the QC covering it).
	//
	// Pruning is asynchronous and MAY BE DELAYED. PruneBefore records the
	// watermark and returns; reclamation happens later, on the
	// implementation's own schedule and potentially at a coarse
	// granularity (e.g. the LittDB implementation reclaims whole segments
	// on its next GC pass, and only after a retention-TTL floor). The
	// watermark guarantees nothing below n is removed before n is
	// reached, but does NOT bound when eligible data is actually
	// reclaimed — pruned entries may remain readable for a while.
	PruneBefore(n GlobalBlockNumber) error

	// Flush blocks until every Write that has returned before Flush is
	// called is durable on disk. Writes made concurrently with Flush
	// may or may not be durable when Flush returns (but are otherwise
	// eventually durable — implementations write to disk on their own
	// schedule even without an explicit Flush).
	//
	// The expected pattern is "write a batch of records, then Flush
	// once" rather than "Flush after every Write." The implementation
	// is free to begin writing as records arrive, so this still
	// batches well even when the caller doesn't pre-buffer.
	//
	// data.State.runPersist will use this: drain every block + QC
	// queued for persistence, write them all, call Flush, then
	// advance nextBlockToPersist (the watermark gating AppVote
	// issuance).
	Flush() error

	// Status returns the block and QC write tips as one consistent snapshot.
	// LastBlockNumber is the highest GlobalBlockNumber accepted by WriteBlock
	// (utils.None if none). LastQCNext is the exclusive upper bound of the last
	// WriteQC — the lowerBound the next contiguous WriteQC must use (utils.None
	// if none).
	//
	// These are the in-memory write cursors used to enforce Write ordering
	// (recovered at open); the call does not perform I/O. Because PruneBefore
	// never removes the newest written cohort, the tips always describe records
	// that are still present and readable: LastBlockNumber equals the highest
	// number a reverse Blocks iterator would yield, and LastQCNext equals
	// GlobalRange().Next of the QC a reverse QCs iterator would yield.
	Status() DBStatus

	// Blocks returns an iterator over every persisted block not yet
	// pruned, for startup replay. Intended to be called once at
	// construction by data.State.NewState.
	//
	// If reverse is false the iterator yields blocks in ascending
	// GlobalBlockNumber order (the efficient direction for a full scan);
	// if reverse is true it yields them newest-first (descending), so a
	// caller can read the most recent block without scanning the whole
	// table. Reverse iteration may incur extra IO when materializing
	// values (see BlockIterator.Block), so prefer forward for full scans.
	//
	// Unlike a bulk read, the iterator materializes one block at a time,
	// so a caller can scan an arbitrarily large retention window without
	// holding it all in memory — and may skip reading the value for
	// blocks it does not need (see BlockIterator.Block).
	//
	// The iterator captures a snapshot of the blocks present when it is
	// created; blocks written afterward are not observed. It is NOT safe
	// for concurrent use and MUST be closed when no longer needed (see
	// BlockIterator.Close).
	Blocks(reverse bool) (BlockIterator, error)

	// QCs returns an iterator over every persisted FullCommitQC not yet
	// pruned. If reverse is false the iterator yields QCs in ascending
	// GlobalRange().First order; if reverse is true it yields them
	// newest-first (descending), so a caller can read the most recent QC
	// without scanning the whole table (reverse value reads may incur
	// extra IO — prefer forward for full scans). Successive forward QCs
	// cover contiguous ranges; the first QC's First is not required to
	// equal committee.FirstBlock() (QCs whose entire range is below the
	// retention watermark have been pruned).
	//
	// Same snapshot, single-goroutine, and must-close semantics as
	// Blocks.
	QCs(reverse bool) (QCIterator, error)

	// ReadBlockByNumber returns the block at GlobalBlockNumber n.
	//
	// The result is one of:
	//   - utils.Some with a nil error: the block is present at n.
	//   - ErrPruned: n is strictly below the current retention watermark. The
	//     block is treated as pruned and is not served while below the
	//     watermark (see ErrPruned for its within-session and cross-restart
	//     semantics).
	//   - utils.None with a nil error: n is at or above the watermark but no
	//     block is present. It was either never written or not yet written —
	//     the two are indistinguishable — and a future write may fill it.
	//
	// Never blocks waiting for a future write; blocking semantics (wait for a
	// write at n) live above this interface, in data.State.
	ReadBlockByNumber(n GlobalBlockNumber) (utils.Option[*Block], error)

	// ReadBlockByHash returns the block whose header hashes to the
	// given value, paired with its GlobalBlockNumber. The hash is the
	// same value as block.Header().Hash() for the block that was passed
	// to WriteBlock.
	//
	// Returns utils.None if no such block is readable — either because
	// none was written or because it has been pruned (see
	// ReadBlockByNumber). Non-blocking.
	ReadBlockByHash(hash BlockHeaderHash) (utils.Option[BlockWithNumber], error)

	// ReadQCByBlockNumber returns the FullCommitQC whose
	// GlobalRange().First ≤ n < GlobalRange().Next — i.e. the QC that
	// finalizes the block at n. Because a single QC covers multiple
	// blocks, the same *FullCommitQC is returned for every n in its
	// range.
	//
	// The result is one of:
	//   - utils.Some with a nil error: a QC covering n is present.
	//   - ErrPruned: n is strictly below the current retention watermark. The
	//     covering QC is treated as pruned and is not served while below the
	//     watermark (see ErrPruned for its within-session and cross-restart
	//     semantics).
	//   - utils.None with a nil error: n is at or above the watermark but no QC
	//     covers it. Either no covering QC was written or it is not yet written.
	//
	// Non-blocking.
	ReadQCByBlockNumber(n GlobalBlockNumber) (utils.Option[*FullCommitQC], error)

	// Close releases resources held by the store. After Close returns,
	// no other method may be called on the BlockDB; doing so is
	// undefined.
	Close() error
}

// DBStatus is the in-memory block and QC write tips returned by BlockDB.Status.
type DBStatus struct {
	LastBlockNumber utils.Option[GlobalBlockNumber]
	LastQCNext      utils.Option[GlobalBlockNumber]
}

// BlockIterator iterates over persisted blocks in GlobalBlockNumber order —
// ascending, or descending if the iterator was created with reverse=true. It
// is created via BlockDB.Blocks and captures a snapshot of the blocks present
// at creation time.
//
// A BlockIterator is NOT safe for concurrent use by multiple goroutines.
type BlockIterator interface {
	// Next advances the iterator to the next block. It returns false when
	// the iteration is complete (no more blocks), and returns an error if
	// advancing failed. After Next returns (false, nil) iteration is
	// complete; after it returns an error the iterator must not be used
	// further (other than Close).
	Next() (bool, error)

	// Number returns the GlobalBlockNumber of the current block. It is only
	// valid to call after Next has returned (true, nil). This is cheap and
	// does not perform IO — a caller can scan numbers and choose which
	// blocks to materialize via Block.
	Number() GlobalBlockNumber

	// Block reads and returns the current block. It is only valid to call
	// after Next has returned (true, nil), and may perform IO (and so
	// return an error). The Block type does not carry its GlobalBlockNumber;
	// pair it with Number.
	Block() (*Block, error)

	// Close releases the resources held by the iterator. MUST be called when
	// done; failure to close may leak resources in disk-backed
	// implementations.
	Close() error
}

// QCIterator iterates over persisted FullCommitQCs in GlobalRange().First
// order — ascending, or descending if the iterator was created with
// reverse=true. It is created via BlockDB.QCs and captures a snapshot of the
// QCs present at creation time.
//
// A QCIterator is NOT safe for concurrent use by multiple goroutines.
type QCIterator interface {
	// Next advances the iterator to the next QC. Same semantics as
	// BlockIterator.Next.
	Next() (bool, error)

	// QC reads and returns the current FullCommitQC. It is only valid to
	// call after Next has returned (true, nil), and may perform IO (and so
	// return an error).
	QC() (*FullCommitQC, error)

	// Close releases the resources held by the iterator. MUST be called when
	// done; failure to close may leak resources in disk-backed
	// implementations.
	Close() error
}

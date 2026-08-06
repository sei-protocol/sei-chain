package types

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// BlockDB is the durable backing store for data.State. It persists the
// finalized records the consensus state machine produces — finalized blocks
// (indexed by GlobalBlockNumber and by header hash), FullCommitQCs (each
// covering a contiguous range of GlobalBlockNumbers), and AppQCs (each matching
// a persisted CommitQC range) — and provides the read API needed for crash
// recovery and runtime lookups.
//
// # Concurrency
//
// All methods are safe for concurrent use. Implementations should expect
// concurrent writes (WriteBlock + WriteQC + WriteAppQC interleaved from a single
// background persistence loop) and concurrent reads from RPC handlers
// and peer-sync streams.
//
// # Durability and crash safety
//
// Writes are two-phase: WriteBlock, WriteQC, and WriteAppQC return without
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
//   - Blocks must be written densely: each block's number must be exactly
//     one greater than the previously written block's (the first block may
//     start anywhere its covering QC allows). WriteBlock returns
//     ErrBlockOutOfOrder otherwise.
//   - QCs must be written contiguously — each QC's GlobalRange().First
//     must equal the previous QC's GlobalRange().Next. WriteQC returns
//     ErrQCNonContiguous otherwise.
//   - QCs must be written before blocks. A QC covering a block must
//     be written before that block is written.
//   - AppQCs must be written contiguously as an exact prefix of retained QCs.
//     The first AppQC starts at the retained QC floor; every AppQC's range must
//     exactly match the next persisted QC range.
//
// After a crash, data not flushed may be lost, but the following invariants hold:
//
//   - Individual blocks, QCs, and AppQCs are either fully persisted or not at all; there are no partial writes.
//   - Data is persisted in order, meaning that data loss never leaves gaps. If A is written and then B
//     is written, then after a crash if B is persisted then A is also persisted.
//   - Since QCs must always be written before the blocks or AppQCs they cover, a persisted block or
//     AppQC is always covered by a persisted QC, but a persisted QC may or may not have its covered
//     blocks or AppQC persisted.
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
	// n must be exactly one greater than the previously written block
	// number (the first block may start anywhere its covering QC allows);
	// otherwise WriteBlock returns ErrBlockOutOfOrder and persists
	// nothing. Writes are NOT idempotent — re-writing the same (or any
	// other non-contiguous) n is rejected with an error.
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

	// WriteQC persists a FullCommitQC. The QC carries the range it covers,
	// so no range is passed alongside it: the covered range is the half-open
	// interval [First, First+len(Headers())), where First is
	// qc.QC().GlobalRange().First. FullCommitQC's own invariant makes that
	// identical to qc.QC().GlobalRange().Next; implementations derive the
	// bound from the header count because First and Headers are the fields
	// the wire format carries explicitly, so the persisted range is stable
	// across an encode/decode round trip. A QC covering no blocks
	// (len(Headers()) == 0) is rejected.
	//
	// Successive WriteQC calls must form a contiguous sequence: each QC's
	// First must equal the previous QC's covered bound (the first QC may
	// start anywhere). A gap or overlap returns ErrQCNonContiguous and
	// persists nothing. Writes are NOT idempotent — re-writing a QC is
	// rejected rather than treated as a no-op.
	//
	// May return before the QC is on disk. See the BlockDB type doc for
	// the two-phase write/flush contract and WriteBlock for the
	// rationale.
	//
	// Writes are made crash durable in write order (both blocks and QCs),
	// so loss of non-durable data after a crash never leaves gaps.
	WriteQC(qc *FullCommitQC) error

	// WriteAppQC persists an AppQC. The AppQC's proposal carries the exact
	// CommitQC range it certifies. A matching CommitQC must already be written:
	// the CommitQC covering GlobalRange.First must have the same GlobalRange.
	//
	// AppQCs form a contiguous prefix aligned with retained CommitQCs. The first
	// AppQC must start at the retained CommitQC floor; each subsequent AppQC's
	// First must equal the previous AppQC's Next. Re-writing, gaps, overlaps,
	// mid-QC starts, and ranges that do not exactly match the next persisted
	// CommitQC range are rejected.
	//
	// May return before the AppQC is on disk. See the BlockDB type doc for the
	// two-phase write/flush contract.
	WriteAppQC(appQC *AppQC) error

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

	// Status returns a consistent snapshot of the in-memory write tips (no I/O).
	Status() DBStatus

	// ReadRecent returns the materialized startup-recovery suffix.
	//
	// Implementations find the newest persisted AppQC, then collect all
	// CommitQCs whose Index is greater than or equal to that AppQC's
	// RoadIndex, and all blocks whose GlobalBlockNumber is greater than or
	// equal to that AppQC's GlobalRange.First. If no AppQC is present,
	// ReadRecent returns all retained CommitQCs and blocks.
	//
	// Returned CommitQCs and Blocks are in ascending GlobalBlockNumber order so
	// data.State can replay them directly. If AppQC is present, CommitQCs
	// starts with its matching CommitQC.
	ReadRecent() (RecentData, error)

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

	// ReadAppQCByBlockNumber returns the AppQC whose
	// AppProposal.GlobalRange().First ≤ n < AppProposal.GlobalRange().Next.
	// Because a single AppQC covers a CommitQC range, the same *AppQC is
	// returned for every n in its range.
	//
	// The result is one of:
	//   - utils.Some with a nil error: an AppQC covering n is present.
	//   - ErrPruned: n is strictly below the current retention watermark.
	//   - utils.None with a nil error: n is at or above the watermark but no
	//     AppQC covers it.
	//
	// Non-blocking.
	ReadAppQCByBlockNumber(n GlobalBlockNumber) (utils.Option[*AppQC], error)

	// Close releases resources held by the store. After Close returns,
	// no other method may be called on the BlockDB; doing so is
	// undefined.
	Close() error
}

// DBStatus is the in-memory write tips returned by BlockDB.Status.
// Both fields are exclusive "next to write" cursors (matching data.State's
// nextQC / nextBlock). Zero means no write of that kind has occurred yet
// (NextBlock/NextQC are never zero after a successful write: the first
// written block number N yields NextBlock = N+1 ≥ 1).
type DBStatus struct {
	// NextBlock is one past the highest GlobalBlockNumber accepted by WriteBlock
	// (the next block number that may be written). Zero if no block has been written.
	NextBlock GlobalBlockNumber
	// NextQC is one past the highest GlobalBlockNumber covered by the last QC
	// accepted by WriteQC (the next QC's range must start here). Zero if no QC
	// has been written.
	NextQC GlobalBlockNumber
	// NextAppQC is one past the highest GlobalBlockNumber covered by the last
	// AppQC accepted by WriteAppQC. Zero if no AppQC has been written.
	NextAppQC GlobalBlockNumber
}

// RecentBlock is one block returned by BlockDB.ReadRecent.
type RecentBlock struct {
	Number GlobalBlockNumber
	Block  *Block
}

// RecentData is the materialized suffix used by data.State startup recovery.
type RecentData struct {
	CommitQCs []*FullCommitQC
	Blocks    []RecentBlock
	AppQC     utils.Option[*AppQC]
}

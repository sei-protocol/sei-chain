package data

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
)

// Store is the durable backing store for data.State. It persists the two
// kinds of finalized records the consensus state machine produces —
// finalized blocks (indexed by GlobalBlockNumber and by header hash) and
// FullCommitQCs (each covering a contiguous range of GlobalBlockNumbers) —
// and provides the read API needed for crash recovery and runtime lookups.
//
// Replaces the WAL-based DataWAL used today; the contract here is a
// superset of what DataWAL.Blocks + DataWAL.CommitQCs provide today, plus
// a by-hash block index the WAL does not have.
//
// # Concurrency
//
// All methods are safe for concurrent use. Implementations should expect
// concurrent writes (WriteBlock + WriteQC interleaved from a single
// background persistence loop) and concurrent reads from RPC handlers
// and peer-sync streams.
//
// # Crash safety
//
// Write* methods are synchronous with respect to durability: a Write
// returns only after the record is durable on disk. A reader after a
// crash either sees the entire write (if it returned) or none of it
// (if it had not yet returned); partial writes are not possible.
//
// Writes are not atomic with respect to one another — a crash between
// two Write calls leaves the earlier one durable and the later one
// absent. Reconciliation of cross-record inconsistencies (e.g. a block
// written without its QC, or vice versa) is the caller's responsibility
// on startup (see DataWAL.reconcile for the rules the current WAL uses).
//
// The synchronous-durability guarantee is what
// data.State.runPersist relies on to advance nextBlockToPersist (and
// thereby unblock PushAppHash → AppVote): once WriteBlock/WriteQC
// return, the data underpinning the next AppVote is on disk.
// Implementations that batch fsyncs internally must still block the
// individual Write call until the batch covering it has been committed.
//
// # Ordering
//
// QCs must be written contiguously — each WriteQC's
// qc.QC().GlobalRange(committee).First must equal the previous WriteQC's
// GlobalRange().Next (the caller is data.State.runPersist, which
// guarantees this). Implementations may validate and reject out-of-order
// writes but need not.
//
// Blocks may be written in any GlobalBlockNumber order; the consumer
// (data.State) writes them in ascending order today but the contract
// does not require it.
type Store interface {
	// WriteBlock persists a finalized block at GlobalBlockNumber n.
	//
	// n is required because *types.Block does NOT carry its
	// GlobalBlockNumber — block.Header().BlockNumber() returns the
	// per-lane BlockNumber, a different typedef. The lane→global
	// mapping lives in the QC's GlobalRange. Implementations must
	// record n alongside the block so ReadBlockByNumber can recover it
	// and ReadAll can reconstruct (n, *Block) pairs.
	//
	// The block's hash (block.Header().Hash()) is indexed automatically
	// so ReadBlockByHash works after this returns — the caller does not
	// supply it separately.
	//
	// Idempotent on duplicate: a second WriteBlock with the same
	// (n, block.Header().Hash()) pair is a no-op. Writing a different
	// block at an already-occupied n, or the same block under a
	// different n, is a contract violation — implementations are free
	// to error or to corrupt state in that case.
	//
	// Returns only after the block is durable on disk. See the Store
	// type doc for the synchronous-durability contract.
	WriteBlock(ctx context.Context, n types.GlobalBlockNumber, block *types.Block) error

	// WriteQC persists a FullCommitQC.
	//
	// The QC carries its GlobalRange internally
	// (qc.QC().GlobalRange(committee)) — no range argument needed. The
	// caller guarantees that successive WriteQC calls form a contiguous
	// sequence: each call's First equals the previous call's Next (or
	// committee.FirstBlock() for the very first call). Implementations
	// may reject out-of-sequence writes but need not.
	//
	// Idempotent on duplicate: a second WriteQC for a QC with the same
	// GlobalRange().First is a no-op.
	//
	// Returns only after the QC is durable on disk. See the Store type
	// doc for the synchronous-durability contract.
	WriteQC(ctx context.Context, qc *types.FullCommitQC) error

	// PruneBefore removes:
	//   - every block with GlobalBlockNumber < n
	//   - every QC whose GlobalRange().Next ≤ n (the QC's entire
	//     covered range is strictly below the retention watermark; a
	//     QC straddling n stays)
	//
	// Idempotent: calling with n ≤ the existing retention watermark is
	// a no-op. Pruning is permitted to be asynchronous — entries may
	// remain readable briefly after PruneBefore returns, but will
	// eventually become unreadable.
	//
	// Callers must ensure no in-flight reader is holding a pointer
	// returned from a Read* call for a record being pruned. Pruning a
	// record still being processed is undefined.
	PruneBefore(ctx context.Context, n types.GlobalBlockNumber) error

	// ReadAll returns a snapshot of all blocks and QCs not yet pruned,
	// for startup replay. Intended to be called once at construction by
	// data.State.NewState; afterwards the in-memory cursors track
	// everything.
	//
	// Blocks are returned in ascending GlobalBlockNumber order, QCs in
	// ascending GlobalRange().First order. The two slices are
	// independent — there is no required alignment between them
	// (DataWAL.reconcile handles cross-WAL drift; the same logic will
	// run over Loaded).
	//
	// May allocate proportional to retention. For typical Sei retention
	// windows this is fine; if a future implementation expects
	// orders-of-magnitude larger retention, consider switching to an
	// iterator API before adopting it.
	ReadAll(ctx context.Context) (*Loaded, error)

	// ReadBlockByNumber returns the block at GlobalBlockNumber n.
	//
	// Returns (nil, false, nil) if no block has been written at n, or
	// the block at n has been pruned. Implementations must not block
	// waiting for a future write — "not yet written" is reported as
	// (nil, false, nil) identical to "never written". Blocking
	// semantics (wait for a write at n) live above this interface, in
	// data.State.
	ReadBlockByNumber(ctx context.Context, n types.GlobalBlockNumber) (*types.Block, bool, error)

	// ReadBlockByHash returns the block whose header hashes to the
	// given value. The hash is the same value as block.Header().Hash()
	// for the block that was passed to WriteBlock.
	//
	// Returns (nil, false, nil) if no such block has been written, or
	// it has been pruned. Like ReadBlockByNumber, this is non-blocking.
	ReadBlockByHash(ctx context.Context, hash types.BlockHeaderHash) (*types.Block, bool, error)

	// ReadQCByBlockNumber returns the FullCommitQC whose
	// GlobalRange().First ≤ n < GlobalRange().Next — i.e. the QC that
	// finalizes the block at n. Because a single QC covers multiple
	// blocks, the same *FullCommitQC is returned for every n in its
	// range.
	//
	// Returns (nil, false, nil) if no QC has been written that covers
	// n yet, or n is below the retention watermark. Non-blocking.
	ReadQCByBlockNumber(ctx context.Context, n types.GlobalBlockNumber) (*types.FullCommitQC, bool, error)

	// Close releases resources held by the store. After Close returns,
	// no other method may be called on the Store; doing so is
	// undefined.
	Close(ctx context.Context) error
}

// Loaded is the result of Store.ReadAll — a point-in-time view of every
// block and QC not yet pruned, used by data.State.NewState to rebuild
// in-memory state at startup.
type Loaded struct {
	// Blocks is the set of persisted blocks in ascending
	// GlobalBlockNumber order. Each entry pairs the block with the
	// GlobalBlockNumber it was written at; the Block type does not
	// carry a global number on its own — its header carries a per-lane
	// BlockNumber, which is distinct.
	Blocks []BlockEntry

	// QCs is the set of persisted FullCommitQCs in ascending
	// GlobalRange().First order. Each QC covers a contiguous range
	// [First, Next); successive entries' ranges are contiguous (no
	// gaps), but the first QC's First is not required to equal
	// committee.FirstBlock() — entries with GlobalRange().Next at or
	// below the retention watermark have been pruned.
	QCs []*types.FullCommitQC
}

// BlockEntry pairs a finalized block with its GlobalBlockNumber. The
// GlobalBlockNumber is the index data.State uses to address the block;
// it is not stored inside *types.Block itself, so the Store records it
// alongside on WriteBlock and returns it on ReadAll.
type BlockEntry struct {
	Number types.GlobalBlockNumber
	Block  *types.Block
}

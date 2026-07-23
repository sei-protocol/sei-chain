package types

import "errors"

// ErrNotFound is returned when a requested record is not yet available —
// a block, QC, or AppProposal ahead of what data.State currently has (e.g.
// ahead of the contiguous block/QC prefix). Distinct from ErrPruned, which
// means the height is below the retention / eviction floor.
var ErrNotFound = errors.New("not found")

// ErrBlockGap is returned when BlockDB blocks are not contiguous (e.g. during
// data.State recovery). That indicates store corruption or an incomplete write
// that left a hole.
var ErrBlockGap = errors.New("block gap in BlockDB")

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

// ErrPruned is returned when a requested record is below the current retention
// / eviction floor and is not served. Used for BlockDB by-number reads below
// the store watermark, and for data.State lookups (blocks, QCs, AppProposals)
// after in-memory eviction. Distinct from utils.None on BlockDB, which means
// "not present at or above the watermark" and may still be filled by a future
// write.
//
// ErrPruned reflects the watermark's current position, not a permanent verdict.
// The watermark only advances while a store stays open, so within a single
// session ErrPruned is terminal — retrying the same n keeps returning it. It is
// not durable across restarts: the watermark is re-derived on open and
// reclamation is asynchronous, so an n that returned ErrPruned before a restart
// may afterward read as present (or as utils.None). Callers should treat
// ErrPruned as "not currently served," not as a guarantee the record is gone.
var ErrPruned = errors.New("pruned: below retention watermark")

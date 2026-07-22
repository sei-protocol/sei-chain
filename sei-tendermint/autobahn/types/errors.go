package types

import "errors"

// ErrNotFound is returned when a requested block/QC/AppProposal is not yet
// available (e.g. ahead of the contiguous prefix in data.State). It is distinct
// from ErrPruned, which means the height is below the retention watermark.
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

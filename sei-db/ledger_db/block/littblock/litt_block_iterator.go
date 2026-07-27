package littblock

import (
	"fmt"

	littdb "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var _ types.BlockDBIterator = (*blockDBIterator)(nil)

// coveredQC is a decoded QC together with the half-open number range it covers.
type coveredQC struct {
	// qc is the decoded QC.
	qc *types.FullCommitQC

	// first is the lowest number the QC covers (its GlobalRange().First).
	first types.GlobalBlockNumber

	// next is one past the highest number the QC covers (its GlobalRange().Next).
	next types.GlobalBlockNumber
}

// blockDBIterator implements types.BlockDBIterator over the shared ledger table.
//
// It steps through consecutive block numbers, driven by a single forward litt scan. The scan
// visits records in insertion order, and the write path inserts every QC before the blocks it
// covers, so by the time the number cursor reaches n the covering QC has always been decoded
// already: the underlying cursor is advanced lazily, collecting QC records into a small pending
// queue and holding position on each block record until the number cursor consumes it.
//
// A nil it represents an empty iterator (produced by IteratorAt when the requested start is not
// covered by any persisted QC): Next reports exhaustion immediately and Close is a no-op.
type blockDBIterator struct {
	// it is the underlying litt scan; nil for an empty iterator.
	it littdb.Iterator

	// watermark is the retention floor captured at creation. Records strictly below it are
	// skipped: blocks may be stranded from their covering QC, and a QC whose whole range is
	// below the floor serves no number (see blockDB.watermark).
	watermark uint64

	// startN is the first number the iterator may yield. Block records below it (a IteratorAt
	// start that lands mid-cohort follows the whole cohort's blocks in the scan) are skipped.
	startN types.GlobalBlockNumber

	// expectStartQC is true when the scan was positioned by IteratorAt, whose first record is the
	// covering QC's primary or covered-number alias rather than a record the normal dispatch
	// handles.
	expectStartQC bool

	// started is false until the first Next call establishes the start position.
	started bool

	// n is the current number; valid while positioned (after Next has returned true).
	n types.GlobalBlockNumber

	// current is the QC covering n; nil only before the first Next call.
	current *coveredQC

	// pending holds QCs the scan has passed whose ranges begin at or above current.next. QCs are
	// written ahead of the blocks they cover, so several can precede the block records that
	// consume them. Ranges are ascending and contiguous with current.
	pending []*coveredQC

	// heldBlock is true when the underlying cursor is positioned on a block record the number
	// cursor has not consumed yet; heldNumber is that record's number.
	heldBlock bool

	// heldNumber is the number of the held block record; meaningful only while heldBlock is true.
	heldNumber types.GlobalBlockNumber

	// exhausted is true once the underlying scan has no more records.
	exhausted bool
}

func (l *blockDBIterator) Next() (bool, error) {
	if l.it == nil {
		return false, nil
	}

	var next types.GlobalBlockNumber
	if l.started {
		next = l.n + 1
		if l.heldBlock && l.heldNumber == l.n {
			// Leaving a position whose block record the scan still holds; release it so the
			// scan can advance past it.
			l.heldBlock = false
		}
	} else {
		// The start position needs the first covering QC in hand before a number can be yielded.
		if err := l.fill(); err != nil {
			return false, err
		}
		if l.current == nil {
			// No retained QC at all: no number is covered.
			return false, nil
		}
		next = l.startN
		if next < l.current.first {
			next = l.current.first
		}
	}

	// Establish coverage for next, promoting across QC range boundaries.
	for next >= l.current.next {
		if len(l.pending) > 0 {
			l.current = l.pending[0]
			l.pending = l.pending[1:]
			continue
		}
		if l.heldBlock {
			// A block record at heldNumber >= next is waiting, but no QC covers next. The write
			// path guarantees a covering QC precedes every block, so this is corruption.
			return false, fmt.Errorf("corrupt store: block %d has no QC coverage", l.heldNumber)
		}
		if l.exhausted {
			return false, nil
		}
		if err := l.fill(); err != nil {
			return false, err
		}
	}

	// Position the scan to answer Block() at next: it must hold the next block record (if any
	// remains) so presence is decidable.
	if !l.heldBlock && !l.exhausted {
		if err := l.fill(); err != nil {
			return false, err
		}
	}
	if l.heldBlock && l.heldNumber != next {
		// Blocks are written densely, so a missing number below the highest persisted block can
		// only be corruption.
		return false, fmt.Errorf("%w: corrupt store: block gap at %d (next persisted block is %d)",
			types.ErrBlockGap, next, l.heldNumber)
	}

	l.n = next
	l.started = true
	return true, nil
}

// fill advances the underlying scan until it holds an unconsumed block record or exhausts,
// decoding every QC record it passes into the covering-QC state.
func (l *blockDBIterator) fill() error {
	for !l.heldBlock && !l.exhausted {
		ok, err := l.it.Next()
		if err != nil {
			return fmt.Errorf("failed to advance ledger scan: %w", err)
		}
		if !ok {
			l.exhausted = true
			return nil
		}
		key, isPrimary := l.it.GetKey()
		switch {
		case l.expectStartQC:
			// IteratorAt positioned the scan at qcKey(start): the first record is the covering
			// QC — its primary when start is a range's First, otherwise a covered-number
			// alias, whose value is the same full QC either way.
			l.expectStartQC = false
			if keyKind(key) != kindQC {
				return fmt.Errorf("ledger scan positioned at %d is not on a QC record", l.startN)
			}
			if err := l.collectQC(); err != nil {
				return err
			}
		case !isPrimary:
			// Secondary records (block hash aliases, QC covered-number aliases) duplicate a
			// primary the scan handles elsewhere.
		case keyKind(key) == kindBlock:
			number := decodeNumberKey(key)
			if uint64(number) < l.watermark || number < l.startN {
				// Below the retention floor (possibly stranded from its covering QC), or below
				// a IteratorAt start that lands mid-cohort.
				continue
			}
			l.heldBlock = true
			l.heldNumber = number
		case keyKind(key) == kindQC:
			if err := l.collectQC(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown ledger key kind %q", keyKind(key))
		}
	}
	return nil
}

// collectQC decodes the QC record at the scan's current position into the covering-QC state: it
// is dropped when its whole range is below the watermark, becomes current when no current QC is
// set, and otherwise joins the pending queue.
func (l *blockDBIterator) collectQC() error {
	value, err := l.it.GetValue()
	if err != nil {
		return fmt.Errorf("failed to read QC value: %w", err)
	}
	qc, err := decodeQC(value)
	if err != nil {
		return fmt.Errorf("failed to unmarshal QC: %w", err)
	}
	// The covered range is First plus one header per covered block. GlobalRange().Next is not
	// used because it is recomputed from lane ranges on decode rather than round-tripped; the
	// header count is the range-size source the rest of littblock trusts (see recoverCursors).
	first := qc.QC().GlobalRange().First
	entry := &coveredQC{qc: qc, first: first, next: first + types.GlobalBlockNumber(len(qc.Headers()))}
	if uint64(entry.next) <= l.watermark {
		// The whole range is below the retention floor: none of its numbers are served.
		return nil
	}
	if l.current == nil {
		l.current = entry
		return nil
	}
	// QCs are written contiguously, so each surviving range must extend the previous bound.
	tailNext := l.current.next
	if len(l.pending) > 0 {
		tailNext = l.pending[len(l.pending)-1].next
	}
	if entry.first != tailNext {
		return fmt.Errorf("corrupt store: QC range [%d,%d) does not extend previous bound %d",
			entry.first, entry.next, tailNext)
	}
	l.pending = append(l.pending, entry)
	return nil
}

func (l *blockDBIterator) Number() types.GlobalBlockNumber {
	return l.n
}

func (l *blockDBIterator) QC() (*types.FullCommitQC, error) {
	return l.current.qc, nil
}

func (l *blockDBIterator) Block() (utils.Option[*types.Block], error) {
	if !l.heldBlock {
		// The tail of the ledger: the covering QC is persisted but this block is not.
		return utils.None[*types.Block](), nil
	}
	value, err := l.it.GetValue()
	if err != nil {
		return utils.None[*types.Block](), fmt.Errorf("failed to read block value: %w", err)
	}
	_, blk, err := decodeBlock(value)
	if err != nil {
		return utils.None[*types.Block](), fmt.Errorf("failed to unmarshal block: %w", err)
	}
	return utils.Some(blk), nil
}

func (l *blockDBIterator) Close() error {
	if l.it == nil {
		return nil
	}
	if err := l.it.Close(); err != nil {
		return fmt.Errorf("failed to close ledger iterator: %w", err)
	}
	return nil
}

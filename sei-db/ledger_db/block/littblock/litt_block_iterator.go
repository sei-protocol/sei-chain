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
// A nil it represents an empty iterator (produced by Iterator when the requested start is past
// the persisted coverage): Next reports exhaustion immediately and Close is a no-op.
type blockDBIterator struct {
	// it is the underlying litt scan; nil for an empty iterator.
	it littdb.Iterator

	// startN is the first number the iterator may yield, and doubles as the retention floor:
	// blockDB.Iterator clamps it up to the prune watermark and to the start of the block history,
	// so a block below startN is either below the start or stranded from a reclaimed QC, and is
	// skipped either way. Because of the block-history floor, startN names a block that exists
	// whenever the store holds one — it may sit inside its covering QC's range rather than on that
	// range's first number.
	startN types.GlobalBlockNumber

	// expectStartQC is one-shot state, not a mode: every non-empty iterator is positioned at
	// qcKey(startN), so the scan's very first record is the covering QC's primary or
	// covered-number alias rather than a record the normal dispatch handles. Cleared once that
	// record is consumed.
	expectStartQC bool

	// started is false until the first Next call establishes the start position.
	started bool

	// positioned is true only while the iterator sits on a number Next yielded. Block
	// rejects calls made when it is false.
	positioned bool

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

	// closed is true once Close has been called. Next and Block reject calls made afterward.
	// Checking it explicitly is what makes that rejection uniform: on a trailing block-less position
	// the scan holds no record, so Next would otherwise skip fill() entirely and hand back a fresh
	// position without ever touching the closed cursor.
	closed bool
}

func (l *blockDBIterator) Next() (types.Position, bool, error) {
	// Any exit other than a yielded position leaves the iterator unpositioned, so a
	// subsequent Block() reports misuse rather than answering for a stale position.
	l.positioned = false

	if l.closed || l.it == nil {
		return types.Position{}, false, nil
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
			return types.Position{}, false, err
		}
		if l.current == nil {
			// Unreachable: the scan is positioned at qcKey(startN), so its first record is the
			// covering QC — fill dispatches it through expectStartQC, which errors if it is not
			// a QC record, and collectQC always adopts it as current because its range contains
			// startN. Asserted rather than dereferenced blindly below.
			return types.Position{}, false,
				fmt.Errorf("ledger scan at %d established no covering QC", l.startN)
		}
		// current's range contains startN (the scan was positioned inside it), so no clamp
		// up to current.first is needed.
		next = l.startN
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
			return types.Position{}, false,
				fmt.Errorf("corrupt store: block %d has no QC coverage", l.heldNumber)
		}
		if l.exhausted {
			return types.Position{}, false, nil
		}
		if err := l.fill(); err != nil {
			return types.Position{}, false, err
		}
	}

	// Position the scan to answer Block() at next: it must hold the next block record (if any
	// remains) so presence is decidable.
	if !l.heldBlock && !l.exhausted {
		if err := l.fill(); err != nil {
			return types.Position{}, false, err
		}
	}
	if l.heldBlock && l.heldNumber != next {
		// Blocks are written densely, so a missing number below the highest persisted block can
		// only be corruption.
		return types.Position{}, false,
			fmt.Errorf("%w: corrupt store: block gap at %d (next persisted block is %d)",
				types.ErrBlockGap, next, l.heldNumber)
	}

	l.n = next
	l.started = true
	l.positioned = true
	return types.Position{Number: next, QC: l.current.qc, HasBlock: l.heldBlock}, true, nil
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
		key, isPrimary, err := l.it.GetKey()
		if err != nil {
			return fmt.Errorf("failed to read ledger key: %w", err)
		}
		switch {
		case l.expectStartQC:
			// The scan was positioned at qcKey(start): the first record is the covering
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
			if number < l.startN {
				// Below the start: either a start that lands mid-cohort, whose cohort's earlier
				// blocks still follow the covering QC in the scan, or a block stranded below the
				// retention floor. startN is never below that floor, so one test covers both.
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
// becomes current when no current QC is set, and otherwise joins the pending queue.
func (l *blockDBIterator) collectQC() error {
	value, err := l.it.GetValue()
	if err != nil {
		return fmt.Errorf("failed to read QC value: %w", err)
	}
	qc, err := decodeQC(value)
	if err != nil {
		return fmt.Errorf("failed to unmarshal QC: %w", err)
	}
	first, next := coveredRange(qc)
	entry := &coveredQC{qc: qc, first: first, next: next}
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

func (l *blockDBIterator) Block() (utils.Option[*types.Block], error) {
	if l.closed {
		return utils.None[*types.Block](), fmt.Errorf("iterator is closed")
	}
	if !l.positioned {
		return utils.None[*types.Block](), fmt.Errorf("iterator is not positioned on a block number")
	}
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
	// A closed iterator holds no position, so Next() reports exhaustion and Block() reports misuse
	// rather than reading through a released snapshot.
	l.closed = true
	l.positioned = false
	if l.it == nil {
		return nil
	}
	if err := l.it.Close(); err != nil {
		return fmt.Errorf("failed to close ledger iterator: %w", err)
	}
	return nil
}

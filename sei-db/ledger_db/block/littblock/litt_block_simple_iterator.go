package littblock

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var _ types.BlockDBIterator = (*simpleIterator)(nil)

// simpleIterator serves one narrow case: a store holding QCs but no blocks at all. That happens between
// the first WriteQC and the first WriteBlock, and after a crash that made a QC durable before any block.
// It yields every number those QCs cover, each with no block.
//
// It exists so blockDBIterator does not have to serve that case. That iterator is driven by a single
// forward scan of the shared ledger table, and this is the one case where the scan has no block to anchor
// its start on: its snapshot can pick up a first block written concurrently and report it as a gap below
// the cursor, since the first block may legally land anywhere inside its covering QC. Handling the case
// here is simpler than building a contract around that behaviour — this iterator takes no table snapshot,
// so there is nothing to race, and the scanning path keeps its plain invariant that iteration starts on a
// block that exists.
//
// Every position is read and decoded up front. Nothing is pinned: no table snapshot, no segment
// reservation, no open handle. Close exists only to satisfy the interface and leaking one costs nothing.
// The case is tiny by construction — no block has been written yet, so coverage spans at most the few
// cohorts a single persistence batch queued.
type simpleIterator struct {
	// positions is every position this iterator will yield, ascending, built at construction.
	positions []types.Position

	// idx is the current position; -1 before the first Next and len(positions) once exhausted.
	idx int

	// closed is true once Close has been called. Block rejects calls made afterward.
	closed bool
}

// newSimpleIterator reads the QCs covering [start, nextQC) and materializes a position for every number
// they cover at or above start, each with no block.
//
// QCs are contiguous, so this walks cohort by cohort: one point read per QC, whose decoded value is
// shared by every position in its range. Sharing matters — types.Position documents QC as the same
// pointer across a range, and callers key "the scan entered a new QC" on that identity.
func newSimpleIterator(
	table qcReader,
	start types.GlobalBlockNumber,
	nextQC types.GlobalBlockNumber,
) (*simpleIterator, error) {
	var positions []types.Position
	for n := start; n < nextQC; {
		qc, err := readQCCovering(table, n)
		if err != nil {
			return nil, err
		}
		first, next := coveredRange(qc)
		if next <= n {
			// Would not advance; a QC whose range does not contain n means coverage is not the
			// contiguous span the write path guarantees.
			return nil, fmt.Errorf("corrupt store: QC at %d covers [%d,%d), which does not reach it",
				n, first, next)
		}
		for m := n; m < next && m < nextQC; m++ {
			positions = append(positions, types.Position{Number: m, QC: qc, HasBlock: false})
		}
		n = next
	}
	return &simpleIterator{positions: positions, idx: -1}, nil
}

func (s *simpleIterator) Next() (types.Position, bool, error) {
	if s.idx < len(s.positions) {
		s.idx++
	}
	if s.closed || s.idx < 0 || s.idx >= len(s.positions) {
		return types.Position{}, false, nil
	}
	return s.positions[s.idx], true, nil
}

// Block always reports absence: this iterator only ever covers numbers whose blocks have not been
// written, so every position it yields has HasBlock false.
func (s *simpleIterator) Block() (utils.Option[*types.Block], error) {
	if s.closed {
		return utils.None[*types.Block](), fmt.Errorf("iterator is closed")
	}
	if s.idx < 0 || s.idx >= len(s.positions) {
		return utils.None[*types.Block](), fmt.Errorf("iterator is not positioned on a block number")
	}
	return utils.None[*types.Block](), nil
}

// Close releases nothing — see the type doc. It only marks the iterator unusable so misuse after close
// is reported rather than silently answered.
func (s *simpleIterator) Close() error {
	s.closed = true
	return nil
}

// qcReader is the slice of littdb.Table that newSimpleIterator needs, so tests can supply QCs without
// building a table.
type qcReader interface {
	Get(key []byte) ([]byte, bool, error)
}

// readQCCovering point-reads and decodes the QC covering n. Every covered number carries a QC alias key
// holding the full QC value, so any number inside a retained range resolves.
func readQCCovering(table qcReader, n types.GlobalBlockNumber) (*types.FullCommitQC, error) {
	value, exists, err := table.Get(qcKey(n))
	if err != nil {
		return nil, fmt.Errorf("failed to read covering QC for %d: %w", n, err)
	}
	if !exists {
		return nil, fmt.Errorf("corrupt store: no QC record at %d despite coverage past it", n)
	}
	qc, err := decodeQC(value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode covering QC for %d: %w", n, err)
	}
	return qc, nil
}

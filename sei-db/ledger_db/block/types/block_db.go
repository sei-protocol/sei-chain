package types

import "fmt"

// RecordKind identifies a primary record or secondary lookup namespace.
// KindBlock and KindQC are lookup-only; QCs and blocks are written as groups.
type RecordKind uint8

const (
	KindBlock RecordKind = iota // block-number lookup only
	KindQC                      // QC lookup only
	KindAppProposal
	KindAppQC
	KindQCGroup
)

// String returns the kind's name.
func (k RecordKind) String() string {
	switch k {
	case KindBlock:
		return "block"
	case KindQCGroup:
		return "qc_group"
	case KindQC:
		return "qc"
	case KindAppProposal:
		return "app_proposal"
	case KindAppQC:
		return "app_qc"
	default:
		return "unknown"
	}
}

// ValidateStandalone checks that the kind can be written without a QC group.
func (k RecordKind) ValidateStandalone() error {
	if k != KindAppProposal && k != KindAppQC {
		return fmt.Errorf("%s cannot be written as a standalone record", k)
	}
	return nil
}

// BlockRecord is an opaque encoded block and its header-hash lookup key.
type BlockRecord struct {
	Hash  []byte
	Value []byte
}

// BlockDB is the storage substrate a consensus block store is built on. It
// holds immutable opaque records addressed by (RecordKind, number), plus a
// by-hash alias for block records, and it owns reclamation.
//
// It ascribes no meaning to a record's bytes or to the numbers it is filed
// under: ordering rules, contiguity, encoding and recovery all belong to the
// layer above.
//
// # Concurrency
//
// All methods are safe for concurrent use, and reads may run concurrently with
// writes and with reclamation.
//
// # Durability
//
// Writes are two-phase: PutQC and PutRecord may return before the record is
// on disk, and Flush blocks until every previously-returned write is durable.
// Reads observe a write as soon as it returns, whether or not it is durable.
//
// Implementations must make writes durable in the order they were issued, so
// that a crash truncates the write sequence rather than punching holes in it.
// The layer above relies on this to guarantee that a surviving record is never
// missing a record written before it.
//
// # Reclamation
//
// A BlockDB reclaims by watermark: SetPruneWatermark moves the floor, and
// records numbered below it become eligible for reclamation. Eligibility is not
// removal — reclamation happens on the implementation's own schedule and at its
// own granularity, so an eligible record may stay readable for some time, and a
// record may be reclaimed while another record below the floor survives. Reads
// are never gated on the floor: a caller that must not serve reclaimable
// records checks GetPruneWatermark itself.
//
// Where the floor may go is not a BlockDB's decision. It moves the floor
// wherever it is told, because the boundaries at which a range of records
// becomes unreadable together are a property of values it cannot read.
//
// # Recovery
//
// The floor is not persisted. On open an implementation re-derives it as the
// number of the oldest KindQCGroup record it still holds, so a restart forgets every
// SetPruneWatermark but never serves a record whose covering QC has already
// been reclaimed. Opening a store that holds records but no KindQCGroup record
// fails: it has lost the records that bound what it may serve.
type BlockDB interface {
	// PutQC atomically stores a QC and all its blocks. The QC is addressable at
	// every number in [first, first+len(blocks)); blocks are in ascending number
	// order and are addressable by number and hash. blocks must be non-empty.
	// Scan presents the group as one KindQCGroup record containing the whole group.
	PutQC(first uint64, qc []byte, blocks []BlockRecord) error

	// PutRecord stores a KindAppProposal or KindAppQC value, addressable at
	// every number in [first, next). Storing an already-stored number is not
	// supported and the result is undefined.
	PutRecord(kind RecordKind, first, next uint64, value []byte) error

	// GetRecord returns the value filed under kind at n, and whether one is
	// present.
	GetRecord(kind RecordKind, n uint64) (value []byte, exists bool, err error)

	// GetBlockByHash returns the block record indexed under hash by PutQC,
	// and whether one is present.
	GetBlockByHash(hash []byte) (value []byte, exists bool, err error)

	// Scan iterates the records in the order they were written, newest first
	// when newestFirst is set and oldest first otherwise. It visits each
	// primary record once at its first number. A PutQC group is returned whole
	// as KindQCGroup; its blocks and lookup aliases are not visited separately.
	// The iterator observes a snapshot of the records present when Scan is called,
	// and must be closed.
	Scan(newestFirst bool) (RecordIterator, error)

	// GetPruneWatermark returns the number below which records are eligible for
	// reclamation, which is 0 until the first SetPruneWatermark advances it.
	GetPruneWatermark() uint64

	// SetPruneWatermark moves the reclamation floor to n verbatim, and does
	// nothing when n is at or below the current floor. The floor only rises, so
	// a record that became eligible stays eligible.
	SetPruneWatermark(n uint64)

	// Flush blocks until every write that returned before the call is durable.
	// Writes made concurrently with Flush may or may not be durable when it
	// returns.
	Flush() error

	// Close releases the resources held by the store. No other method may be
	// called afterwards.
	Close() error
}

// RecordIterator walks the records of a BlockDB. It is created by BlockDB.Scan
// and is not safe for concurrent use.
type RecordIterator interface {
	// Next advances to the next record, reporting false once the scan is
	// complete. After it returns false or an error, only Close may be called.
	Next() (bool, error)

	// Kind returns the current record's kind. Valid only after Next returned
	// true.
	Kind() RecordKind

	// Number returns the number the current record was filed under. Valid only
	// after Next returned true.
	Number() uint64

	// Value returns the current record's bytes, which must not be modified.
	// Valid only after Next returned true.
	Value() ([]byte, error)

	// Close releases the iterator's resources. It MUST be called; failing to
	// do so may leak disk.
	Close() error
}

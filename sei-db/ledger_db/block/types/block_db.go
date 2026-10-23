package types

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

// BlockNumber identifies a block in the ledger.
type BlockNumber uint64

// QCRecord contains a QC's version and unprefixed serialized data.
type QCRecord struct {
	Version RecordVersion
	Data    []byte
}

// QCGroupRecord contains a QC's version, unprefixed serialized data, and numbered blocks.
type QCGroupRecord struct {
	Version RecordVersion
	Data    []byte
	Blocks  []BlockRecord
}

// AppQCRecord contains an AppQC's version, unprefixed serialized data, and block range.
type AppQCRecord struct {
	Version RecordVersion
	Data    []byte
	First   BlockNumber
	Next    BlockNumber
}

// AppProposalRecord contains an AppProposal's version, unprefixed serialized data, and block range.
type AppProposalRecord struct {
	Version RecordVersion
	Data    []byte
	First   BlockNumber
	Next    BlockNumber
}

// BlockRecord is an opaque encoded block with its number and header-hash lookup key.
type BlockRecord struct {
	Number BlockNumber
	Hash   []byte
	Value  []byte
}

// Suffix contains whole records in increasing order within each kind.
type Suffix struct {
	QCs          []QCRecord
	Blocks       []BlockRecord
	AppQCs       []AppQCRecord
	AppProposals []AppProposalRecord
}

// BlockDB is the storage substrate a consensus block store is built on. It
// holds immutable opaque records addressed by (RecordKind, number), plus a
// by-hash alias for block records, and it owns reclamation.
//
// Record payloads are opaque; contiguity and payload encoding belong to the
// layer above.
//
// # Concurrency
//
// All methods are safe for concurrent use, and reads may run concurrently with
// writes and with reclamation.
//
// # Durability
//
// Writes are two-phase: PutQCGroup, PutAppQC and PutAppProposal may return before
// their records are on disk. Flush blocks until every previously-returned
// write is durable.
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
	// PutQCGroup atomically stores qc and its blocks. QC lookups resolve at each block's
	// Number; blocks resolve by Number and Hash. qc.Blocks must be non-empty and
	// ordered by increasing Number. The group is indexed by qc.Blocks[0].Number.
	// ReadSuffix returns the QC and block records of each retained group.
	PutQCGroup(qc QCGroupRecord) error

	// PutAppQC stores record at every number in [record.First, record.Next).
	// Storing an already-stored number is unsupported and the result is undefined.
	PutAppQC(record AppQCRecord) error

	// PutAppProposal stores record at every number in [record.First, record.Next).
	// Storing an already-stored number is unsupported and the result is undefined.
	PutAppProposal(record AppProposalRecord) error

	// GetQC returns the QC covering n.
	GetQC(n BlockNumber) (QCRecord, bool, error)
	// GetBlock returns the block numbered n.
	GetBlock(n BlockNumber) (BlockRecord, bool, error)
	// GetBlockByHash returns the block indexed by hash.
	GetBlockByHash(hash []byte) (BlockRecord, bool, error)
	// GetAppQC returns the application QC covering n.
	GetAppQC(n BlockNumber) (AppQCRecord, bool, error)
	// GetAppProposal returns the application proposal covering n.
	GetAppProposal(n BlockNumber) (AppProposalRecord, bool, error)
	// ReadSuffix returns a snapshot starting at the newest AppQC's group,
	// or all retained records when no AppQC exists. Returned bytes are immutable.
	ReadSuffix() (Suffix, error)

	// GetPruneWatermark returns the number below which records are eligible for
	// reclamation, which is 0 until the first SetPruneWatermark advances it.
	GetPruneWatermark() BlockNumber

	// SetPruneWatermark moves the reclamation floor to n verbatim, and does
	// nothing when n is at or below the current floor. The floor only rises, so
	// a record that became eligible stays eligible.
	SetPruneWatermark(n BlockNumber)

	// Flush blocks until every write that returned before the call is durable.
	// Writes made concurrently with Flush may or may not be durable when it
	// returns.
	Flush() error

	// Close releases the resources held by the store. No other method may be
	// called afterwards.
	Close() error
}

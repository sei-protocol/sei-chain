package types

// BlockNumber identifies a block in the ledger.
type BlockNumber uint64

// BlockHash identifies a block by its 32-byte header hash.
type BlockHash [32]byte

// BlockRecord contains a SuperBlock's storage index, hash, QC, and lane blocks.
type BlockRecord struct {
	Index      BlockNumber
	Hash       BlockHash
	QC         []byte
	LaneBlocks [][]byte
}

// AppQCRecord contains an AppQC's serialized data and road index.
type AppQCRecord struct {
	Index BlockNumber
	Data  []byte
}

// AppProposalRecord contains an AppProposal's serialized data and road index.
type AppProposalRecord struct {
	Index BlockNumber
	Data  []byte
}

// Suffix contains whole records in increasing order within each kind.
type Suffix struct {
	Blocks       []BlockRecord
	AppQCs       []AppQCRecord
	AppProposals []AppProposalRecord
}

// BlockDB is the storage substrate a consensus block store is built on. It
// holds immutable opaque records addressed by block number, plus a
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
// Writes are two-phase: PutBlock, PutAppQC and PutAppProposal may return before
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
// number of the oldest SuperBlock it still holds, so a restart forgets every
// SetPruneWatermark but never serves a record whose QC has already
// been reclaimed. Opening a store that holds app records but no SuperBlock
// fails: it has lost the records that bound what it may serve.
type BlockDB interface {
	// PutBlock atomically stores a SuperBlock's QC and lane blocks in header order.
	// LaneBlocks may be empty. Rewriting an index or hash
	// is unsupported. ReadSuffix includes the complete record.
	PutBlock(record BlockRecord) error

	// PutAppProposal stores the application proposal at record.Index.
	// Rewriting an index is unsupported.
	PutAppProposal(record AppProposalRecord) error

	// PutAppQC stores the application QC at record.Index.
	// Rewriting an index is unsupported.
	PutAppQC(record AppQCRecord) error

	// GetQC returns the serialized QC at n.
	GetQC(n BlockNumber) ([]byte, bool, error)
	// GetBlockByNumber returns the complete SuperBlock record at n.
	GetBlockByNumber(n BlockNumber) (BlockRecord, bool, error)
	// GetBlockByHash returns the complete SuperBlock record indexed by hash.
	GetBlockByHash(hash BlockHash) (BlockRecord, bool, error)
	// GetLaneBlock returns a lane block at its header offset within SuperBlock n.
	// A missing SuperBlock or out-of-range offset returns false.
	GetLaneBlock(n BlockNumber, offset uint64) ([]byte, bool, error)
	// GetAppProposal returns the serialized application proposal at n.
	GetAppProposal(n BlockNumber) ([]byte, bool, error)
	// GetAppQC returns the serialized application QC at n.
	GetAppQC(n BlockNumber) ([]byte, bool, error)
	// ReadSuffix returns a snapshot starting at the newest AppQC's SuperBlock,
	// or all retained records when no AppQC exists. Returned records are immutable.
	ReadSuffix() (Suffix, error)

	// GetPruneWatermark returns the number below which records are eligible for
	// reclamation. A fresh store starts at 0; reopening restores the oldest retained index.
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

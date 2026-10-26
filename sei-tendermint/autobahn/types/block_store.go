package types

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// BlockStore is the durable backing store for data.State. It persists complete
// CommitQC groups, indexed by RoadIndex and SuperBlock hash, and execution records.
// All methods are safe for concurrent use.
//
// Writes are visible immediately. Flush makes all previously completed writes
// durable. A crash can lose an unflushed suffix, but surviving records have no
// gaps: a CommitQC and all its lane blocks survive together, and application
// records never survive without their preceding QC group and app records.
//
// Each write stream must advance by one RoadIndex. At each road, WriteQC must
// precede WriteAppProposal, which must precede WriteAppQC. Rejected writes store
// nothing. Reads below First return ErrPruned; absent records at or above First
// return None. Hash lookups return None for unknown or pruned SuperBlocks.
type BlockStore interface {
	// WriteQC stores a CommitQC and its complete lane blocks in header order.
	// Gaps and overlaps return ErrUnexpectedIndex. Missing, extra,
	// reordered, or mismatched blocks return ErrQCBlocksMismatch.
	WriteQC(qc *FullCommitQC, blocks []*Block) error
	WriteAppQC(qc *AppQC) error
	WriteAppProposal(proposal *AppProposal) error
	// PruneBefore advances the retention floor, capped at the latest recovery anchor.
	// Before the first AppQC it retains the first QC group; afterward it retains
	// at least the latest full row (QC, lane blocks, AppProposal, AppQC).
	// Reclamation is asynchronous. The floor only rises within a session, but is
	// re-derived on reopen, so previously hidden records may become readable again.
	PruneBefore(n RoadIndex) error
	First() RoadIndex
	Flush() error
	Status() utils.Option[SuffixRange]
	// ReadSuffix returns complete QC groups in road order with lane blocks in
	// header order. It starts at the latest AppQC, or the first retained QC when
	// no AppQC exists, and includes all later groups and app records.
	ReadSuffix() (Suffix, error)
	ReadLaneBlock(pos LaneBlockPos) (utils.Option[*Block], error)
	ReadSuperBlockByNumber(n RoadIndex) (utils.Option[*SuperBlock], error)
	ReadSuperBlockByHash(hash common.Hash) (utils.Option[*SuperBlock], error)
	ReadQCByBlockNumber(n RoadIndex) (utils.Option[*FullCommitQC], error)
	ReadAppProposalByBlockNumber(n RoadIndex) (utils.Option[*AppProposal], error)
	ReadAppQCByBlockNumber(n RoadIndex) (utils.Option[*AppQC], error)
	Close() error
}

// SuffixRange contains the road cursors restored by startup recovery.
// First <= NextAppQC <= NextAppProposal <= NextQC.
type SuffixRange struct {
	// First is the latest AppQC's road, or the first retained QC's road if no AppQC exists.
	First RoadIndex
	// NextAppQC is the next road awaiting a persisted AppQC.
	NextAppQC RoadIndex
	// NextAppProposal is the next road awaiting a persisted AppProposal.
	NextAppProposal RoadIndex
	// NextQC is the next road awaiting a persisted QC group.
	NextQC RoadIndex
}

// QCGroup is a committed QC and all its blocks in header order.
type QCGroup struct {
	QC     *FullCommitQC
	Blocks []*Block
}

// Suffix is the materialized suffix used by data.State startup recovery.
type Suffix struct {
	// Ranges of elements in the suffix.
	// None if the BlockStore is empty.
	Status utils.Option[SuffixRange]
	// Elements which constitute the suffix.
	QCGroups     []QCGroup
	AppProposals []*AppProposal
	AppQCs       []*AppQC
}

package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// BlockStore is the durable backing store for data.State. It persists complete
// CommitQC groups, indexed by RoadIndex and road block header hash, and execution records.
// All methods are safe for concurrent use.
//
// Writes are visible immediately. Flush makes all previously completed writes
// durable. A crash can lose an unflushed suffix, but surviving records have no
// gaps: a CommitQC and all its lane blocks survive together, and application
// records never survive without their preceding QC group and app records.
//
// Each write stream must advance by one RoadIndex. At each road, WriteBlock must
// precede WriteAppProposal, which must precede WriteAppQC. Rejected writes store
// nothing. Reads below First return ErrPruned; absent records at or above First
// return None. Hash lookups return None for unknown or pruned SuperBlocks.
type BlockStore interface {
	// WriteBlock stores a road block with its QC and lane blocks in header order.
	// The block must be verified by the caller. Gaps and overlaps return ErrUnexpectedIndex.
	WriteBlock(block *RoadBlock) error
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
	ReadBlock(n RoadIndex) (utils.Option[*RoadBlock], error)
	ReadBlockByHash(hash RoadBlockHeaderHash) (utils.Option[*RoadBlock], error)
	ReadQC(n RoadIndex) (utils.Option[*FullCommitQC], error)
	ReadAppProposal(n RoadIndex) (utils.Option[*AppProposal], error)
	ReadAppQC(n RoadIndex) (utils.Option[*AppQC], error)
	Close() error
}

// SuffixRange contains the road cursors restored by startup recovery.
// First <= NextAppQC <= NextAppProposal <= NextBlock.
type SuffixRange struct {
	// First is the latest AppQC's road, or the first retained QC's road if no AppQC exists.
	First RoadIndex
	// NextAppQC is the next road awaiting a persisted AppQC.
	NextAppQC RoadIndex
	// NextAppProposal is the next road awaiting a persisted AppProposal.
	NextAppProposal RoadIndex
	// NextBlock is the next road awaiting a persisted block.
	NextBlock RoadIndex
}

// RoadBlock is a committed QC and all its blocks in header order.
type RoadBlock struct {
	utils.ReadOnly
	qc         *FullCommitQC
	laneBlocks []*Block
}

// NewRoadBlock constructs a road block with lane blocks in header order.
func NewRoadBlock(qc *FullCommitQC, laneBlocks []*Block) *RoadBlock {
	return &RoadBlock{qc: qc, laneBlocks: laneBlocks}
}

// Header returns the road block header.
func (b *RoadBlock) Header() *RoadBlockHeader { return b.qc.Header() }

// QC returns the full CommitQC for the road.
func (b *RoadBlock) QC() *FullCommitQC { return b.qc }

// LaneBlocks returns the lane blocks in header order.
func (b *RoadBlock) LaneBlocks() []*Block { return b.laneBlocks }

// SuperBlock returns the merged transactions and execution metadata for the road.
func (b *RoadBlock) SuperBlock() *SuperBlock {
	superBlock := &SuperBlock{
		Hash:      b.Header().Hash(),
		Height:    b.qc.Index(),
		Timestamp: b.Header().Timestamp(),
	}
	for _, block := range b.laneBlocks {
		superBlock.GasWanted += block.Payload().TotalGasWanted()
		superBlock.Txs = append(superBlock.Txs, block.Payload().Txs()...)
	}
	return superBlock
}

// Verify verifies the QC against the epoch and checks its lane blocks in header order.
func (b *RoadBlock) Verify(ep *Epoch) error {
	if err := b.qc.Verify(ep); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	if len(b.laneBlocks) != len(b.qc.Headers()) {
		return ErrQCBlocksMismatch
	}
	for i, block := range b.laneBlocks {
		if err := block.Verify(); err != nil {
			return fmt.Errorf("block %d: %w: %w", i, ErrQCBlocksMismatch, err)
		}
		if block.Header().Hash() != b.qc.Headers()[i].Hash() {
			return ErrQCBlocksMismatch
		}
	}
	return nil
}

// Suffix is the materialized suffix used by data.State startup recovery.
type Suffix struct {
	// Ranges of elements in the suffix.
	// None if the BlockStore is empty.
	Status utils.Option[SuffixRange]
	// Elements which constitute the suffix.
	Blocks       []*RoadBlock
	AppProposals []*AppProposal
	AppQCs       []*AppQC
}

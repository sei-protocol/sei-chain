package types

import "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"

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

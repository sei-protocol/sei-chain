package blocktest

import "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"

// BlockRecord contains a SuperBlock's storage index, hash, QC, and lane blocks.
type BlockRecord struct {
	Index      types.RoadIndex
	Hash       types.RoadBlockHeaderHash
	QC         []byte
	LaneBlocks [][]byte
}

// AppQCRecord contains an AppQC's serialized data and road index.
type AppQCRecord struct {
	Index types.RoadIndex
	Data  []byte
}

// AppProposalRecord contains an AppProposal's serialized data and road index.
type AppProposalRecord struct {
	Index types.RoadIndex
	Data  []byte
}

// Suffix contains whole records in increasing order within each kind.
type Suffix struct {
	Blocks       []BlockRecord
	AppQCs       []AppQCRecord
	AppProposals []AppProposalRecord
}

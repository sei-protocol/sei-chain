package types

import (
	"fmt"

	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/protocol"
)

// CommitQC .
type CommitQC struct {
	utils.ReadOnly
	vote *Hashed[*CommitVote]
	sigs []*Signature
}

// NewCommitQC constructs a new CommitQC.
func NewCommitQC(votes []*Signed[*CommitVote]) *CommitQC {
	if len(votes) == 0 {
		panic("qc cannot be empty")
	}
	sigs := make([]*Signature, len(votes))
	for i, v := range votes {
		sigs[i] = v.sig
	}
	return &CommitQC{vote: votes[0].hashed, sigs: sigs}
}

// Proposal .
func (m *CommitQC) Proposal() *Proposal { return m.vote.Msg().proposal }

// Index .
func (m *CommitQC) Index() RoadIndex {
	return m.Proposal().Index()
}

// LaneRange returns the range of lane blocks.
func (m *CommitQC) LaneRange(lane LaneID) *LaneRange {
	return m.Proposal().LaneRange(lane)
}

// GlobalRange returns the finalized global block range.
func (m *CommitQC) GlobalRange() GlobalRange {
	return m.Proposal().GlobalRange()
}

// Verify verifies the CommitQC against the committee.
// Currently it doesn't require the previous CommitQC.
func (m *CommitQC) Verify(c *Committee) error {
	return m.vote.verifyQC(c, c.CommitQuorum(), m.sigs)
}

// FullCommitQC is a CommitQC with the headers of the blocks finalized by it.
type FullCommitQC struct {
	utils.ReadOnly
	qc      *CommitQC
	headers []*BlockHeader
}

// NewFullCommitQC constructs a new FullCommitQC.
func NewFullCommitQC(qc *CommitQC, headers []*BlockHeader) *FullCommitQC {
	if gr := qc.GlobalRange(); len(headers) != int(gr.Next-gr.First) {
		panic(fmt.Sprintf("headers length %d != global range %d", len(headers), gr.Next-gr.First))
	}
	return &FullCommitQC{qc: qc, headers: headers}
}

// QC CommitQC.
func (m *FullCommitQC) QC() *CommitQC { return m.qc }

// Headers of the blocks finalized by the QC.
func (m *FullCommitQC) Headers() []*BlockHeader { return m.headers }

// Index .
func (m *FullCommitQC) Index() RoadIndex {
	return m.qc.Index()
}

// Verify verifies the FullCommitQC against the committee.
func (m *FullCommitQC) Verify(c *Committee) error {
	if err := m.qc.Verify(c); err != nil {
		return fmt.Errorf("qC: %w", err)
	}
	n := uint64(0)
	if want, got := int(m.qc.GlobalRange().Len()), len(m.headers); want != got {
		return fmt.Errorf("len(headers) = %d, want %d", got, want)
	}
	for _, lane := range c.Lanes().All() {
		lr := m.qc.LaneRange(lane)
		if lr.Len() == 0 {
			continue
		}
		n += lr.Len()
		want := lr.LastHash()
		for i := range lr.Len() {
			if got := m.headers[n-i-1].Hash(); got != want {
				return fmt.Errorf("header[%d].Hash() = %v, want %v", i, got, want)
			}
			want = m.headers[n-i-1].ParentHash()
		}
	}
	return nil
}

// CommitQCConv is a protobuf converter for CommitQC.
var CommitQCConv = utils.ProtoConv[*CommitQC, *protocol.CommitQC]{
	Encode: func(m *CommitQC) *protocol.CommitQC {
		return &protocol.CommitQC{
			Vote: CommitVoteConv.Encode(m.vote.Msg()),
			Sigs: SignatureConv.EncodeSlice(m.sigs),
		}
	},
	Decode: func(m *protocol.CommitQC) (*CommitQC, error) {
		vote, err := CommitVoteConv.DecodeReq(m.Vote)
		if err != nil {
			return nil, fmt.Errorf("vote: %w", err)
		}
		sigs, err := SignatureConv.DecodeSlice(m.Sigs)
		if err != nil {
			return nil, fmt.Errorf("sigs: %w", err)
		}
		return &CommitQC{vote: NewHashed(vote), sigs: sigs}, nil
	},
}

// FullCommitQCConv is a protobuf converter for FullCommitQC.
var FullCommitQCConv = utils.ProtoConv[*FullCommitQC, *protocol.FullCommitQC]{
	Encode: func(m *FullCommitQC) *protocol.FullCommitQC {
		return &protocol.FullCommitQC{
			Qc:      CommitQCConv.Encode(m.qc),
			Headers: BlockHeaderConv.EncodeSlice(m.headers),
		}
	},
	Decode: func(m *protocol.FullCommitQC) (*FullCommitQC, error) {
		qc, err := CommitQCConv.DecodeReq(m.Qc)
		if err != nil {
			return nil, fmt.Errorf("qC: %w", err)
		}
		headers, err := BlockHeaderConv.DecodeSlice(m.Headers)
		if err != nil {
			return nil, fmt.Errorf("headers: %w", err)
		}
		return &FullCommitQC{qc: qc, headers: headers}, nil
	},
}

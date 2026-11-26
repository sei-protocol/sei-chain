package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
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

// Header returns the committed road block header.
func (m *CommitQC) Header() *RoadBlockHeader { return m.vote.Msg().Header() }

// View returns the commit vote's consensus view.
func (m *CommitQC) View() View { return m.vote.Msg().View() }

// Height .
func (m *CommitQC) Height() RoadHeight {
	return m.vote.Msg().Header().Height()
}

// LaneRange returns the range of lane blocks.
func (m *CommitQC) LaneRange(lane LaneID) *LaneRange {
	return m.Header().LaneRange(lane)
}

// Verify checks that the header of the CommitQC is valid in ep and follows parent, and that a
// commit quorum of the committee of ep signed the CommitQC. parent is the CommitQC of the previous
// road block, or None when the CommitQC finalizes the first road block of the chain.
func (m *CommitQC) Verify(ep *Epoch, parent utils.Option[*CommitQC]) error {
	if err := m.Header().VerifyWithParent(ep, parent); err != nil {
		return err
	}
	c := ep.Committee()
	return m.vote.verifyQC(c, c.CommitQuorum(), m.sigs)
}

// verifyWithoutParent checks that the header of the CommitQC is valid in ep and that a commit
// quorum of the committee of ep signed the CommitQC. It does not check the previous road block.
func (m *CommitQC) verifyWithoutParent(ep *Epoch) error {
	if err := m.vote.Msg().Verify(ep); err != nil {
		return err
	}
	c := ep.Committee()
	return m.vote.verifyQC(c, c.CommitQuorum(), m.sigs)
}

// FullCommitQC is a CommitQC with the headers of the blocks finalized by it.
type FullCommitQC struct {
	utils.ReadOnly
	qc      *CommitQC
	headers []*LaneBlockHeader
}

// NewFullCommitQC constructs a new FullCommitQC.
func NewFullCommitQC(qc *CommitQC, headers []*LaneBlockHeader) *FullCommitQC {
	if got, want := len(headers), int(qc.Header().NumBlocks()); got != want { //nolint:gosec // total lane range len is a small bounded value representing block count in a QC
		//TODO(gprusak): why is it ok to panic here?
		panic(fmt.Errorf("headers length %d != finalized blocks %d", got, want))
	}
	return &FullCommitQC{qc: qc, headers: headers}
}

// QC CommitQC.
func (m *FullCommitQC) QC() *CommitQC { return m.qc }

// Header returns the committed road block header.
func (m *FullCommitQC) Header() *RoadBlockHeader { return m.qc.Header() }

// Headers of the blocks finalized by the QC.
func (m *FullCommitQC) Headers() []*LaneBlockHeader { return m.headers }

// Height .
func (m *FullCommitQC) Height() RoadHeight {
	return m.qc.Height()
}

// Verify checks the CommitQC of the FullCommitQC with CommitQC.Verify(ep, parent), and checks
// that the lane block headers of the FullCommitQC are the ones that the CommitQC finalizes.
func (m *FullCommitQC) Verify(ep *Epoch, parent utils.Option[*CommitQC]) error {
	if err := m.qc.Verify(ep, parent); err != nil {
		return fmt.Errorf("qC: %w", err)
	}
	return m.verifyHeaders(ep)
}

// verifyWithoutParent checks the CommitQC of the FullCommitQC against ep without a parent, and
// checks that the lane block headers of the FullCommitQC are the ones that the CommitQC finalizes.
func (m *FullCommitQC) verifyWithoutParent(ep *Epoch) error {
	if err := m.qc.verifyWithoutParent(ep); err != nil {
		return fmt.Errorf("qC: %w", err)
	}
	return m.verifyHeaders(ep)
}

// verifyHeaders checks that the lane block headers of the FullCommitQC are the ones that the lane
// ranges of its CommitQC finalize, for each lane of the committee of ep.
func (m *FullCommitQC) verifyHeaders(ep *Epoch) error {
	n := uint64(0)
	if want, got := int(m.Header().NumBlocks()), len(m.headers); want != got { //nolint:gosec // lane block count is bounded by the committee size and per-lane limit
		return fmt.Errorf("len(headers) = %d, want %d", got, want)
	}
	for lane := range ep.Committee().Lanes().All() {
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
var CommitQCConv = protoutils.Conv[*CommitQC, *pb.CommitQC]{
	Encode: func(m *CommitQC) *pb.CommitQC {
		return &pb.CommitQC{
			VoteV2: CommitVoteConv.Encode(m.vote.Msg()),
			Sigs:   SignatureConv.EncodeSlice(m.sigs),
		}
	},
	Decode: func(m *pb.CommitQC) (*CommitQC, error) {
		vote, err := CommitVoteConv.DecodeReq(m.VoteV2)
		if err != nil {
			return nil, fmt.Errorf("vote_v2: %w", err)
		}
		sigs, err := SignatureConv.DecodeSlice(m.Sigs)
		if err != nil {
			return nil, fmt.Errorf("sigs: %w", err)
		}
		return &CommitQC{vote: NewHashed(vote), sigs: sigs}, nil
	},
}

// FullCommitQCConv is a protobuf converter for FullCommitQC.
var FullCommitQCConv = protoutils.Conv[*FullCommitQC, *pb.FullCommitQC]{
	Encode: func(m *FullCommitQC) *pb.FullCommitQC {
		return &pb.FullCommitQC{
			Qc:        CommitQCConv.Encode(m.qc),
			HeadersV2: LaneBlockHeaderConv.EncodeSlice(m.headers),
		}
	},
	Decode: func(m *pb.FullCommitQC) (*FullCommitQC, error) {
		qc, err := CommitQCConv.DecodeReq(m.Qc)
		if err != nil {
			return nil, fmt.Errorf("qc: %w", err)
		}
		headers, err := LaneBlockHeaderConv.DecodeSlice(m.HeadersV2)
		if err != nil {
			return nil, fmt.Errorf("headers_v2: %w", err)
		}
		return &FullCommitQC{qc: qc, headers: headers}, nil
	},
}

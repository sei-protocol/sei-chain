package consensus

import (
	"errors"
	"fmt"

	cstypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/consensus/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/bits"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmcons "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/consensus"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// Message defines an interface that the consensus domain types implement. When
// a proto message is received on a consensus p2p Channel, it is wrapped and then
// converted to a Message via MsgFromProto.
type Message interface {
	ValidateBasic() error
}

// NewRoundStepMessage is sent for every step taken in the ConsensusState.
// For every height/round/step transition
type NewRoundStepMessage struct {
	cstypes.HRS
	SecondsSinceStartTime int64
	LastCommitRound       int32
}

// ValidateBasic performs basic validation.
func (m *NewRoundStepMessage) ValidateBasic() error {
	if m.Height < 0 {
		return errors.New("negative Height")
	}
	if m.Round < 0 {
		return errors.New("negative Round")
	}
	if !m.Step.IsValid() {
		return errors.New("invalid Step")
	}

	// NOTE: SecondsSinceStartTime may be negative

	// LastCommitRound will be -1 for the initial height, but we don't know what height this is
	// since it can be specified in genesis. The reactor will have to validate this via
	// ValidateHeight().
	if m.LastCommitRound < -1 {
		return errors.New("invalid LastCommitRound (cannot be < -1)")
	}

	return nil
}

// ValidateHeight validates the height given the chain's initial height.
func (m *NewRoundStepMessage) ValidateHeight(initialHeight int64) error {
	if m.Height < initialHeight {
		return fmt.Errorf("invalid Height %v (lower than initial height %v)",
			m.Height, initialHeight)
	}
	if m.Height == initialHeight && m.LastCommitRound != -1 {
		return fmt.Errorf("invalid LastCommitRound %v (must be -1 for initial height %v)",
			m.LastCommitRound, initialHeight)
	}
	if m.Height > initialHeight && m.LastCommitRound < 0 {
		return fmt.Errorf("LastCommitRound can only be negative for initial height %v",
			initialHeight)
	}
	return nil
}

// String returns a string representation.
func (m *NewRoundStepMessage) String() string {
	return fmt.Sprintf("[NewRoundStep H:%v R:%v S:%v LCR:%v]",
		m.Height, m.Round, m.Step, m.LastCommitRound)
}

// NewValidBlockMessage is sent when a validator observes a valid block B in some round r,
// i.e., there is a Proposal for block B and 2/3+ prevotes for the block B in the round r.
// In case the block is also committed, then IsCommit flag is set to true.
type NewValidBlockMessage struct {
	Height             int64
	Round              int32
	BlockPartSetHeader types.PartSetHeader
	BlockParts         *bits.BitArray
	IsCommit           bool
}

// ValidateBasic performs basic validation.
func (m *NewValidBlockMessage) ValidateBasic() error {
	if m.Height < 0 {
		return errors.New("negative Height")
	}
	if m.Round < 0 {
		return errors.New("negative Round")
	}
	if err := m.BlockPartSetHeader.ValidateBasic(); err != nil {
		return fmt.Errorf("wrong BlockPartSetHeader: %w", err)
	}
	if m.BlockParts.Size() == 0 {
		return errors.New("empty blockParts")
	}
	if m.BlockParts.Size() != int(m.BlockPartSetHeader.Total) {
		return fmt.Errorf("blockParts bit array size %d not equal to BlockPartSetHeader.Total %d",
			m.BlockParts.Size(),
			m.BlockPartSetHeader.Total)
	}
	if m.BlockParts.Size() > int(types.MaxBlockPartsCount) {
		return fmt.Errorf("blockParts bit array is too big: %d, max: %d", m.BlockParts.Size(), types.MaxBlockPartsCount)
	}
	return nil
}

// String returns a string representation.
func (m *NewValidBlockMessage) String() string {
	return fmt.Sprintf("[ValidBlockMessage H:%v R:%v BP:%v BA:%v IsCommit:%v]",
		m.Height, m.Round, m.BlockPartSetHeader, m.BlockParts, m.IsCommit)
}

// ProposalMessage is sent when a new block is proposed.
type ProposalMessage struct {
	Proposal *types.Proposal
}

// ValidateBasic performs basic validation.
func (m *ProposalMessage) ValidateBasic() error {
	return m.Proposal.ValidateBasic()
}

// String returns a string representation.
func (m *ProposalMessage) String() string {
	return fmt.Sprintf("[Proposal %v]", m.Proposal)
}

// ProposalPOLMessage is sent when a previous proposal is re-proposed.
type ProposalPOLMessage struct {
	Height           int64
	ProposalPOLRound int32
	ProposalPOL      *bits.BitArray
}

// ValidateBasic performs basic validation.
func (m *ProposalPOLMessage) ValidateBasic() error {
	if m.Height < 0 {
		return errors.New("negative Height")
	}
	if m.ProposalPOLRound < 0 {
		return errors.New("negative ProposalPOLRound")
	}
	if m.ProposalPOL.Size() == 0 {
		return errors.New("empty ProposalPOL bit array")
	}
	if m.ProposalPOL.Size() > types.MaxVotesCount {
		return fmt.Errorf("proposalPOL bit array is too big: %d, max: %d", m.ProposalPOL.Size(), types.MaxVotesCount)
	}
	return nil
}

// String returns a string representation.
func (m *ProposalPOLMessage) String() string {
	return fmt.Sprintf("[ProposalPOL H:%v POLR:%v POL:%v]", m.Height, m.ProposalPOLRound, m.ProposalPOL)
}

// BlockPartMessage is sent when gossipping a piece of the proposed block.
type BlockPartMessage struct {
	Height int64
	Round  int32
	Part   *types.Part
}

// ValidateBasic performs basic validation.
func (m *BlockPartMessage) ValidateBasic() error {
	if m.Height < 0 {
		return errors.New("negative Height")
	}
	if m.Round < 0 {
		return errors.New("negative Round")
	}
	if err := m.Part.ValidateBasic(); err != nil {
		return fmt.Errorf("wrong Part: %w", err)
	}
	return nil
}

// String returns a string representation.
func (m *BlockPartMessage) String() string {
	return fmt.Sprintf("[BlockPart H:%v R:%v P:%v]", m.Height, m.Round, m.Part)
}

// VoteMessage is sent when voting for a proposal (or lack thereof).
type VoteMessage struct {
	Vote *types.Vote
}

// ValidateBasic checks whether the vote within the message is well-formed.
func (m *VoteMessage) ValidateBasic() error {
	return m.Vote.ValidateBasic()
}

// String returns a string representation.
func (m *VoteMessage) String() string {
	return fmt.Sprintf("[Vote %v]", m.Vote)
}

// HasVoteMessage is sent to indicate that a particular vote has been received.
type HasVoteMessage struct {
	Height int64
	Round  int32
	Type   tmproto.SignedMsgType
	Index  int32
}

// ValidateBasic performs basic validation.
func (m *HasVoteMessage) ValidateBasic() error {
	if m.Height < 0 {
		return errors.New("negative Height")
	}
	if m.Round < 0 {
		return errors.New("negative Round")
	}
	if !types.IsVoteTypeValid(m.Type) {
		return errors.New("invalid Type")
	}
	if m.Index < 0 {
		return errors.New("negative Index")
	}
	return nil
}

// String returns a string representation.
func (m *HasVoteMessage) String() string {
	return fmt.Sprintf("[HasVote VI:%v V:{%v/%02d/%v}]", m.Index, m.Height, m.Round, m.Type)
}

// VoteSetMaj23Message is sent to indicate that a given BlockID has seen +2/3 votes.
type VoteSetMaj23Message struct {
	Height  int64
	Round   int32
	Type    tmproto.SignedMsgType
	BlockID types.BlockID
}

// ValidateBasic performs basic validation.
func (m *VoteSetMaj23Message) ValidateBasic() error {
	if m.Height < 0 {
		return errors.New("negative Height")
	}
	if m.Round < 0 {
		return errors.New("negative Round")
	}
	if !types.IsVoteTypeValid(m.Type) {
		return errors.New("invalid Type")
	}
	if err := m.BlockID.ValidateBasic(); err != nil {
		return fmt.Errorf("wrong BlockID: %w", err)
	}

	return nil
}

// String returns a string representation.
func (m *VoteSetMaj23Message) String() string {
	return fmt.Sprintf("[VSM23 %v/%02d/%v %v]", m.Height, m.Round, m.Type, m.BlockID)
}

// VoteSetBitsMessage is sent to communicate the bit-array of votes seen for the
// BlockID.
type VoteSetBitsMessage struct {
	Height  int64
	Round   int32
	Type    tmproto.SignedMsgType
	BlockID types.BlockID
	Votes   *bits.BitArray
}

// ValidateBasic performs basic validation.
func (m *VoteSetBitsMessage) ValidateBasic() error {
	if m.Height < 0 {
		return errors.New("negative Height")
	}
	if !types.IsVoteTypeValid(m.Type) {
		return errors.New("invalid Type")
	}
	if err := m.BlockID.ValidateBasic(); err != nil {
		return fmt.Errorf("wrong BlockID: %w", err)
	}

	// NOTE: Votes.Size() can be zero if the node does not have any
	if m.Votes.Size() > types.MaxVotesCount {
		return fmt.Errorf("votes bit array is too big: %d, max: %d", m.Votes.Size(), types.MaxVotesCount)
	}

	return nil
}

// String returns a string representation.
func (m *VoteSetBitsMessage) String() string {
	return fmt.Sprintf("[VSB %v/%02d/%v %v %v]", m.Height, m.Round, m.Type, m.BlockID, m.Votes)
}

func newRoundStepMessageFromProto(pb *tmcons.NewRoundStep) (*NewRoundStepMessage, error) {
	step, ok := utils.SafeCast[cstypes.RoundStepType](pb.Step)
	if !ok {
		return nil, fmt.Errorf("denying message due to possible overflow")
	}
	msg := &NewRoundStepMessage{
		HRS: cstypes.HRS{
			Height: pb.Height,
			Round:  pb.Round,
			Step:   step,
		},
		SecondsSinceStartTime: pb.SecondsSinceStartTime,
		LastCommitRound:       pb.LastCommitRound,
	}
	return msg, msg.ValidateBasic()
}

func newValidBlockMessageFromProto(pb *tmcons.NewValidBlock) (*NewValidBlockMessage, error) {
	blockPartSetHeader, err := types.PartSetHeaderFromProto(&pb.BlockPartSetHeader)
	if err != nil {
		return nil, fmt.Errorf("BlockPartSetHeader: %w", err)
	}
	blockParts := new(bits.BitArray)
	if err := blockParts.FromProto(pb.BlockParts); err != nil {
		return nil, fmt.Errorf("BlockParts: %w", err)
	}
	msg := &NewValidBlockMessage{
		Height:             pb.Height,
		Round:              pb.Round,
		BlockPartSetHeader: *blockPartSetHeader,
		BlockParts:         blockParts,
		IsCommit:           pb.IsCommit,
	}
	return msg, msg.ValidateBasic()
}

func proposalMessageFromProto(pb *tmcons.Proposal) (*ProposalMessage, error) {
	proposal, err := types.ProposalFromProto(&pb.Proposal)
	if err != nil {
		return nil, fmt.Errorf("Proposal: %w", err)
	}
	msg := &ProposalMessage{
		Proposal: proposal,
	}
	return msg, msg.ValidateBasic()
}

func proposalPOLMessageFromProto(pb *tmcons.ProposalPOL) (*ProposalPOLMessage, error) {
	proposalPOL := new(bits.BitArray)
	if err := proposalPOL.FromProto(&pb.ProposalPol); err != nil {
		return nil, fmt.Errorf("ProposalPol: %w", err)
	}
	msg := &ProposalPOLMessage{
		Height:           pb.Height,
		ProposalPOLRound: pb.ProposalPolRound,
		ProposalPOL:      proposalPOL,
	}
	return msg, msg.ValidateBasic()
}

func blockPartMessageFromProto(pb *tmcons.BlockPart) (*BlockPartMessage, error) {
	part, err := types.PartFromProto(&pb.Part)
	if err != nil {
		return nil, fmt.Errorf("Part: %w", err)
	}
	msg := &BlockPartMessage{
		Height: pb.Height,
		Round:  pb.Round,
		Part:   part,
	}
	return msg, msg.ValidateBasic()
}

func voteMessageFromProto(pb *tmcons.Vote) (*VoteMessage, error) {
	vote, err := types.VoteFromProto(pb.Vote)
	if err != nil {
		return nil, fmt.Errorf("Vote: %w", err)
	}
	msg := &VoteMessage{
		Vote: vote,
	}
	return msg, msg.ValidateBasic()
}

func hasVoteMessageFromProto(pb *tmcons.HasVote) (*HasVoteMessage, error) {
	msg := &HasVoteMessage{
		Height: pb.Height,
		Round:  pb.Round,
		Type:   pb.Type,
		Index:  pb.Index,
	}
	return msg, msg.ValidateBasic()
}

func voteSetMaj23MessageFromProto(pb *tmcons.VoteSetMaj23) (*VoteSetMaj23Message, error) {
	blockID, err := types.BlockIDFromProto(&pb.BlockID)
	if err != nil {
		return nil, fmt.Errorf("BlockID: %w", err)
	}
	msg := &VoteSetMaj23Message{
		Height:  pb.Height,
		Round:   pb.Round,
		Type:    pb.Type,
		BlockID: *blockID,
	}
	return msg, msg.ValidateBasic()
}

func voteSetBitsMessageFromProto(pb *tmcons.VoteSetBits) (*VoteSetBitsMessage, error) {
	blockID, err := types.BlockIDFromProto(&pb.BlockID)
	if err != nil {
		return nil, fmt.Errorf("BlockID: %w", err)
	}
	votes := new(bits.BitArray)
	if err := votes.FromProto(&pb.Votes); err != nil {
		return nil, fmt.Errorf("votes to proto error: %w", err)
	}
	msg := &VoteSetBitsMessage{
		Height:  pb.Height,
		Round:   pb.Round,
		Type:    pb.Type,
		BlockID: *blockID,
		Votes:   votes,
	}
	return msg, msg.ValidateBasic()
}

func (msg *NewRoundStepMessage) ToProto() *tmcons.NewRoundStep {
	return &tmcons.NewRoundStep{
		Height:                msg.Height,
		Round:                 msg.Round,
		Step:                  uint32(msg.Step),
		SecondsSinceStartTime: msg.SecondsSinceStartTime,
		LastCommitRound:       msg.LastCommitRound,
	}
}

func (msg *NewValidBlockMessage) ToProto() *tmcons.NewValidBlock {
	return &tmcons.NewValidBlock{
		Height:             msg.Height,
		Round:              msg.Round,
		BlockPartSetHeader: msg.BlockPartSetHeader.ToProto(),
		BlockParts:         msg.BlockParts.ToProto(),
		IsCommit:           msg.IsCommit,
	}
}

func (msg *ProposalMessage) ToProto() *tmcons.Proposal {
	return &tmcons.Proposal{
		Proposal: *msg.Proposal.ToProto(),
	}
}

func (msg *ProposalPOLMessage) ToProto() *tmcons.ProposalPOL {
	return &tmcons.ProposalPOL{
		Height:           msg.Height,
		ProposalPolRound: msg.ProposalPOLRound,
		ProposalPol:      *msg.ProposalPOL.ToProto(),
	}
}

func (msg *BlockPartMessage) ToProto() *tmcons.BlockPart {
	return &tmcons.BlockPart{
		Height: msg.Height,
		Round:  msg.Round,
		Part:   *msg.Part.ToProto(),
	}
}

func (msg *VoteMessage) ToProto() *tmcons.Vote {
	return &tmcons.Vote{
		Vote: msg.Vote.ToProto(),
	}
}

func (msg *HasVoteMessage) ToProto() *tmcons.HasVote {
	return &tmcons.HasVote{
		Height: msg.Height,
		Round:  msg.Round,
		Type:   msg.Type,
		Index:  msg.Index,
	}
}

func (msg *VoteSetMaj23Message) ToProto() *tmcons.VoteSetMaj23 {
	return &tmcons.VoteSetMaj23{
		Height:  msg.Height,
		Round:   msg.Round,
		Type:    msg.Type,
		BlockID: msg.BlockID.ToProto(),
	}
}

func (msg *VoteSetBitsMessage) ToProto() *tmcons.VoteSetBits {
	pb := &tmcons.VoteSetBits{
		Height:  msg.Height,
		Round:   msg.Round,
		Type:    msg.Type,
		BlockID: msg.BlockID.ToProto(),
	}
	if bits := msg.Votes.ToProto(); bits != nil {
		pb.Votes = *bits
	}
	return pb
}

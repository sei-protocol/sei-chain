package tmschemas_test

import (
	"testing"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard/tmschemas"
	bcproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/blocksync"
	tmcons "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/consensus"
	ssproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/statesync"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// Test helpers.

func marshal(t *testing.T, m gogoproto.Message) []byte {
	t.Helper()
	bz, err := gogoproto.Marshal(m)
	require.NoError(t, err)
	return bz
}

func commitWith(n int) *tmproto.Commit {
	return &tmproto.Commit{Signatures: make([]tmproto.CommitSig, n)}
}

func evidenceWithCommit(c *tmproto.Commit) tmproto.Evidence {
	return tmproto.Evidence{Sum: &tmproto.Evidence_LightClientAttackEvidence{
		LightClientAttackEvidence: &tmproto.LightClientAttackEvidence{
			ConflictingBlock: &tmproto.LightBlock{
				SignedHeader: &tmproto.SignedHeader{Commit: c},
			},
		},
	}}
}

func evidenceList(commits ...*tmproto.Commit) []tmproto.Evidence {
	out := make([]tmproto.Evidence, len(commits))
	for i, c := range commits {
		out[i] = evidenceWithCommit(c)
	}
	return out
}

// --- Blocksync ---

func blocksyncResponse(lastCommit *tmproto.Commit, evidenceCommits ...*tmproto.Commit) *bcproto.Message {
	return &bcproto.Message{Sum: &bcproto.Message_BlockResponse{
		BlockResponse: &bcproto.BlockResponse{
			Block: &tmproto.Block{
				LastCommit: lastCommit,
				Evidence:   tmproto.EvidenceList{Evidence: evidenceList(evidenceCommits...)},
			},
		},
	}}
}

func TestBlocksync_AcceptsLastCommitAtCap(t *testing.T) {
	require.NoError(t, tmschemas.ValidateBlocksyncMessage(marshal(t,
		blocksyncResponse(commitWith(tmschemas.MaxCommitSignatures)))))
}

func TestBlocksync_AcceptsEvidenceCommitAtCap(t *testing.T) {
	require.NoError(t, tmschemas.ValidateBlocksyncMessage(marshal(t,
		blocksyncResponse(nil, commitWith(tmschemas.MaxCommitSignatures)))))
}

func TestBlocksync_RejectsLastCommitOverCap(t *testing.T) {
	require.Error(t, tmschemas.ValidateBlocksyncMessage(marshal(t,
		blocksyncResponse(commitWith(tmschemas.MaxCommitSignatures+1)))))
}

func TestBlocksync_RejectsEvidenceOverCap(t *testing.T) {
	require.Error(t, tmschemas.ValidateBlocksyncMessage(marshal(t,
		blocksyncResponse(nil, commitWith(tmschemas.MaxCommitSignatures+1)))))
}

func TestBlocksync_SharedBudgetAcrossLastCommitAndEvidence(t *testing.T) {
	// last_commit + evidence Commit signatures share one budget — combined
	// over cap rejects, even though each individually is under cap.
	half := tmschemas.MaxCommitSignatures/2 + 1
	require.Error(t, tmschemas.ValidateBlocksyncMessage(marshal(t,
		blocksyncResponse(commitWith(half), commitWith(half)))))
}

func TestBlocksync_EvidenceCommitsShareBudget(t *testing.T) {
	half := tmschemas.MaxCommitSignatures/2 + 1
	require.Error(t, tmschemas.ValidateBlocksyncMessage(marshal(t,
		blocksyncResponse(nil, commitWith(half), commitWith(half)))))
}

func TestBlocksync_IgnoresBlockRequest(t *testing.T) {
	msg := &bcproto.Message{Sum: &bcproto.Message_BlockRequest{
		BlockRequest: &bcproto.BlockRequest{Height: 42},
	}}
	require.NoError(t, tmschemas.ValidateBlocksyncMessage(marshal(t, msg)))
}

func TestBlocksync_RejectsDuplicateNonRepeatedFields(t *testing.T) {
	// Hand-encode two last_commit entries each at the cap. gogoproto.Marshal
	// won't produce this shape (Block.last_commit is non-repeated), so we
	// build the wire bytes directly to verify the cumulative counter
	// rejects the merged result.
	commit := emptyCommitWire(tmschemas.MaxCommitSignatures)
	lastCommitField := wireguard.MustFieldNum[tmproto.Block]("last_commit")
	block := protowire.AppendTag(nil, lastCommitField, protowire.BytesType)
	block = protowire.AppendVarint(block, uint64(len(commit)))
	block = append(block, commit...)
	block = protowire.AppendTag(block, lastCommitField, protowire.BytesType)
	block = protowire.AppendVarint(block, uint64(len(commit)))
	block = append(block, commit...)

	// Wrap in BlockResponse (field 1) then Message.block_response (field 3).
	blockResp := protowire.AppendTag(nil, 1, protowire.BytesType)
	blockResp = protowire.AppendVarint(blockResp, uint64(len(block)))
	blockResp = append(blockResp, block...)
	msg := protowire.AppendTag(nil, 3, protowire.BytesType)
	msg = protowire.AppendVarint(msg, uint64(len(blockResp)))
	msg = append(msg, blockResp...)

	require.Error(t, tmschemas.ValidateBlocksyncMessage(msg))
}

// emptyCommitWire builds the wire-format bytes for a Commit with n empty
// CommitSig entries.
func emptyCommitWire(n int) []byte {
	field := wireguard.MustFieldNum[tmproto.Commit]("signatures")
	var commit []byte
	for i := 0; i < n; i++ {
		commit = protowire.AppendTag(commit, field, protowire.BytesType)
		commit = protowire.AppendVarint(commit, 0)
	}
	return commit
}

// --- Consensus DataChannel + reassembled block ---

func consensusProposalMessage(lastCommit *tmproto.Commit, evidenceCommits ...*tmproto.Commit) *tmcons.Message {
	inner := tmproto.Proposal{
		LastCommit: lastCommit,
		Evidence:   &tmproto.EvidenceList{Evidence: evidenceList(evidenceCommits...)},
	}
	return &tmcons.Message{Sum: &tmcons.Message_Proposal{
		Proposal: &tmcons.Proposal{Proposal: inner},
	}}
}

func TestConsensusDataChannel_AcceptsLastCommitAtCap(t *testing.T) {
	require.NoError(t, tmschemas.ValidateConsensusDataChannel(marshal(t,
		consensusProposalMessage(commitWith(tmschemas.MaxCommitSignatures)))))
}

func TestConsensusDataChannel_RejectsLastCommitOverCap(t *testing.T) {
	require.Error(t, tmschemas.ValidateConsensusDataChannel(marshal(t,
		consensusProposalMessage(commitWith(tmschemas.MaxCommitSignatures+1)))))
}

func TestConsensusDataChannel_RejectsEvidenceOverCap(t *testing.T) {
	require.Error(t, tmschemas.ValidateConsensusDataChannel(marshal(t,
		consensusProposalMessage(nil, commitWith(tmschemas.MaxCommitSignatures+1)))))
}

func TestConsensusDataChannel_SharedBudgetAcrossLastCommitAndEvidence(t *testing.T) {
	half := tmschemas.MaxCommitSignatures/2 + 1
	require.Error(t, tmschemas.ValidateConsensusDataChannel(marshal(t,
		consensusProposalMessage(commitWith(half), commitWith(half)))))
}

func TestConsensusDataChannel_EvidenceCommitsShareBudget(t *testing.T) {
	half := tmschemas.MaxCommitSignatures/2 + 1
	require.Error(t, tmschemas.ValidateConsensusDataChannel(marshal(t,
		consensusProposalMessage(nil, commitWith(half), commitWith(half)))))
}

func TestConsensusDataChannel_PassesNonProposal(t *testing.T) {
	msg := &tmcons.Message{Sum: &tmcons.Message_BlockPart{
		BlockPart: &tmcons.BlockPart{
			Height: 1, Round: 0,
			Part: tmproto.Part{Index: 0, Bytes: []byte{1, 2, 3}},
		},
	}}
	require.NoError(t, tmschemas.ValidateConsensusDataChannel(marshal(t, msg)))
}

func consensusAssembledBlock(lastCommit *tmproto.Commit, evidenceCommits ...*tmproto.Commit) *tmproto.Block {
	return &tmproto.Block{
		LastCommit: lastCommit,
		Evidence:   tmproto.EvidenceList{Evidence: evidenceList(evidenceCommits...)},
	}
}

func TestConsensusAssembledBlock_AcceptsLastCommitAtCap(t *testing.T) {
	require.NoError(t, tmschemas.ValidateConsensusAssembledBlock(marshal(t,
		consensusAssembledBlock(commitWith(tmschemas.MaxCommitSignatures)))))
}

func TestConsensusAssembledBlock_RejectsLastCommitOverCap(t *testing.T) {
	require.Error(t, tmschemas.ValidateConsensusAssembledBlock(marshal(t,
		consensusAssembledBlock(commitWith(tmschemas.MaxCommitSignatures+1)))))
}

func TestConsensusAssembledBlock_RejectsEvidenceOverCap(t *testing.T) {
	require.Error(t, tmschemas.ValidateConsensusAssembledBlock(marshal(t,
		consensusAssembledBlock(nil, commitWith(tmschemas.MaxCommitSignatures+1)))))
}

func TestConsensusAssembledBlock_SharedBudgetAcrossLastCommitAndEvidence(t *testing.T) {
	half := tmschemas.MaxCommitSignatures/2 + 1
	require.Error(t, tmschemas.ValidateConsensusAssembledBlock(marshal(t,
		consensusAssembledBlock(commitWith(half), commitWith(half)))))
}

// --- Evidence channel ---

func lcaeEvidence(n int) *tmproto.Evidence {
	ev := evidenceWithCommit(commitWith(n))
	return &ev
}

func TestEvidence_AcceptsAtCap(t *testing.T) {
	require.NoError(t, tmschemas.ValidateEvidenceMessage(marshal(t, lcaeEvidence(tmschemas.MaxCommitSignatures))))
}

func TestEvidence_RejectsOverCap(t *testing.T) {
	require.Error(t, tmschemas.ValidateEvidenceMessage(marshal(t, lcaeEvidence(tmschemas.MaxCommitSignatures+1))))
}

func TestEvidence_PassesDuplicateVoteEvidence(t *testing.T) {
	ev := &tmproto.Evidence{Sum: &tmproto.Evidence_DuplicateVoteEvidence{
		DuplicateVoteEvidence: &tmproto.DuplicateVoteEvidence{},
	}}
	require.NoError(t, tmschemas.ValidateEvidenceMessage(marshal(t, ev)))
}

// --- Statesync LightBlock channel ---

func lightBlockRespMsg(n int) *ssproto.Message {
	return &ssproto.Message{Sum: &ssproto.Message_LightBlockResponse{
		LightBlockResponse: &ssproto.LightBlockResponse{
			LightBlock: &tmproto.LightBlock{
				SignedHeader: &tmproto.SignedHeader{Commit: commitWith(n)},
			},
		},
	}}
}

func TestStatesync_AcceptsAtCap(t *testing.T) {
	require.NoError(t, tmschemas.ValidateStatesyncLightBlockChannel(marshal(t, lightBlockRespMsg(tmschemas.MaxCommitSignatures))))
}

func TestStatesync_RejectsOverCap(t *testing.T) {
	require.Error(t, tmschemas.ValidateStatesyncLightBlockChannel(marshal(t, lightBlockRespMsg(tmschemas.MaxCommitSignatures+1))))
}

func TestStatesync_PassesRequest(t *testing.T) {
	msg := &ssproto.Message{Sum: &ssproto.Message_LightBlockRequest{
		LightBlockRequest: &ssproto.LightBlockRequest{Height: 42},
	}}
	require.NoError(t, tmschemas.ValidateStatesyncLightBlockChannel(marshal(t, msg)))
}

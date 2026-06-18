package consensus_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard/wgtest"
	tmcons "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/consensus"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func consensusProposalMessage(lastCommit *tmproto.Commit, evidenceCommits ...*tmproto.Commit) *tmcons.Message {
	inner := tmproto.Proposal{
		LastCommit: lastCommit,
		Evidence:   &tmproto.EvidenceList{Evidence: wgtest.EvidenceList(evidenceCommits...)},
	}
	return &tmcons.Message{Sum: &tmcons.Message_Proposal{
		Proposal: &tmcons.Proposal{Proposal: inner},
	}}
}

func TestSchemaForMessage_AcceptsLastCommitAtCap(t *testing.T) {
	require.NoError(t, tmcons.SchemaForMessage.Scan(wgtest.Marshal(t,
		consensusProposalMessage(wgtest.CommitWith(wgtest.MaxCommitSignatures)))))
}

func TestSchemaForMessage_RejectsLastCommitOverCap(t *testing.T) {
	require.Error(t, tmcons.SchemaForMessage.Scan(wgtest.Marshal(t,
		consensusProposalMessage(wgtest.CommitWith(wgtest.MaxCommitSignatures+1)))))
}

func TestSchemaForMessage_RejectsEvidenceOverCap(t *testing.T) {
	require.Error(t, tmcons.SchemaForMessage.Scan(wgtest.Marshal(t,
		consensusProposalMessage(nil, wgtest.CommitWith(wgtest.MaxCommitSignatures+1)))))
}

func TestSchemaForMessage_MultipleCommitsEachAtCapPass(t *testing.T) {
	// Each Commit is checked independently (per-instance). Two commits each
	// at exactly MaxCommitSignatures must both pass.
	require.NoError(t, tmcons.SchemaForMessage.Scan(wgtest.Marshal(t,
		consensusProposalMessage(wgtest.CommitWith(wgtest.MaxCommitSignatures),
			wgtest.CommitWith(wgtest.MaxCommitSignatures)))))
}

func TestSchemaForMessage_PassesNonProposal(t *testing.T) {
	msg := &tmcons.Message{Sum: &tmcons.Message_BlockPart{
		BlockPart: &tmcons.BlockPart{
			Height: 1, Round: 0,
			Part: tmproto.Part{Index: 0, Bytes: []byte{1, 2, 3}},
		},
	}}
	require.NoError(t, tmcons.SchemaForMessage.Scan(wgtest.Marshal(t, msg)))
}

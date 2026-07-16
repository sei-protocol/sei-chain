package consensus_test

import (
	"testing"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	tmcons "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/consensus"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const maxCommitSignatures = types.MaxVotesCount

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

func consensusProposalMessage(lastCommit *tmproto.Commit, evidenceCommits ...*tmproto.Commit) *tmcons.Message {
	inner := tmproto.Proposal{
		LastCommit: lastCommit,
		Evidence:   &tmproto.EvidenceList{Evidence: evidenceList(evidenceCommits...)},
	}
	return &tmcons.Message{Sum: &tmcons.Message_Proposal{
		Proposal: &tmcons.Proposal{Proposal: inner},
	}}
}

func TestSchemaForMessage_AcceptsLastCommitAtCap(t *testing.T) {
	require.NoError(t, protoutils.Scan[*tmcons.Message](marshal(t,
		consensusProposalMessage(commitWith(maxCommitSignatures)))))
}

func TestSchemaForMessage_RejectsLastCommitOverCap(t *testing.T) {
	require.Error(t, protoutils.Scan[*tmcons.Message](marshal(t,
		consensusProposalMessage(commitWith(maxCommitSignatures+1)))))
}

func TestSchemaForMessage_RejectsEvidenceOverCap(t *testing.T) {
	require.Error(t, protoutils.Scan[*tmcons.Message](marshal(t,
		consensusProposalMessage(nil, commitWith(maxCommitSignatures+1)))))
}

func TestSchemaForMessage_LastCommitAndEvidenceHaveSeparateBudgets(t *testing.T) {
	half := maxCommitSignatures/2 + 1
	require.NoError(t, protoutils.Scan[*tmcons.Message](marshal(t,
		consensusProposalMessage(commitWith(half), commitWith(half)))))
}

func TestSchemaForMessage_EvidenceCommitsHaveSeparateBudgets(t *testing.T) {
	half := maxCommitSignatures/2 + 1
	require.NoError(t, protoutils.Scan[*tmcons.Message](marshal(t,
		consensusProposalMessage(nil, commitWith(half), commitWith(half)))))
}

func TestSchemaForMessage_PassesNonProposal(t *testing.T) {
	msg := &tmcons.Message{Sum: &tmcons.Message_BlockPart{
		BlockPart: &tmcons.BlockPart{
			Height: 1, Round: 0,
			Part: tmproto.Part{Index: 0, Bytes: []byte{1, 2, 3}},
		},
	}}
	require.NoError(t, protoutils.Scan[*tmcons.Message](marshal(t, msg)))
}

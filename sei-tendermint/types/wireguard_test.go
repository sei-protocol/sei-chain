package types_test

import (
	"testing"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
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

func consensusAssembledBlock(lastCommit *tmproto.Commit, evidenceCommits ...*tmproto.Commit) *tmproto.Block {
	return &tmproto.Block{
		LastCommit: lastCommit,
		Evidence:   tmproto.EvidenceList{Evidence: evidenceList(evidenceCommits...)},
	}
}

func lcaeEvidence(n int) *tmproto.Evidence {
	ev := evidenceWithCommit(commitWith(n))
	return &ev
}

func TestSchemaForBlock_AcceptsLastCommitAtCap(t *testing.T) {
	require.NoError(t, protoutils.Scan[*tmproto.Block](marshal(t,
		consensusAssembledBlock(commitWith(maxCommitSignatures)))))
}

func TestSchemaForBlock_RejectsLastCommitOverCap(t *testing.T) {
	require.Error(t, protoutils.Scan[*tmproto.Block](marshal(t,
		consensusAssembledBlock(commitWith(maxCommitSignatures+1)))))
}

func TestSchemaForBlock_RejectsEvidenceOverCap(t *testing.T) {
	require.Error(t, protoutils.Scan[*tmproto.Block](marshal(t,
		consensusAssembledBlock(nil, commitWith(maxCommitSignatures+1)))))
}

func TestSchemaForBlock_LastCommitAndEvidenceHaveSeparateBudgets(t *testing.T) {
	half := maxCommitSignatures/2 + 1
	require.NoError(t, protoutils.Scan[*tmproto.Block](marshal(t,
		consensusAssembledBlock(commitWith(half), commitWith(half)))))
}

func TestSchemaForEvidence_AcceptsAtCap(t *testing.T) {
	require.NoError(t, protoutils.Scan[*tmproto.Evidence](marshal(t, lcaeEvidence(maxCommitSignatures))))
}

func TestSchemaForEvidence_RejectsOverCap(t *testing.T) {
	require.Error(t, protoutils.Scan[*tmproto.Evidence](marshal(t, lcaeEvidence(maxCommitSignatures+1))))
}

func TestSchemaForEvidence_PassesDuplicateVoteEvidence(t *testing.T) {
	ev := &tmproto.Evidence{Sum: &tmproto.Evidence_DuplicateVoteEvidence{
		DuplicateVoteEvidence: &tmproto.DuplicateVoteEvidence{},
	}}
	require.NoError(t, protoutils.Scan[*tmproto.Evidence](marshal(t, ev)))
}

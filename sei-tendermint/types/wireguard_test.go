package types_test

import (
	"testing"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"

	autobahnTypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const (
	maxCommitSignatures = types.MaxVotesCount
	maxValidators       = autobahnTypes.MaxValidators
	maxTxsPerBlock      = int(autobahnTypes.MaxTxsPerBlock)
)

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

func TestSchemaForEvidenceList_AcceptsAtCap(t *testing.T) {
	list := &tmproto.EvidenceList{Evidence: make([]tmproto.Evidence, maxValidators)}
	require.NoError(t, protoutils.Scan[*tmproto.EvidenceList](marshal(t, list)))
}

func TestSchemaForEvidenceList_RejectsOverCap(t *testing.T) {
	list := &tmproto.EvidenceList{Evidence: make([]tmproto.Evidence, maxValidators+1)}
	require.Error(t, protoutils.Scan[*tmproto.EvidenceList](marshal(t, list)))
}

func makeValidators(n int) []*tmproto.Validator {
	vs := make([]*tmproto.Validator, n)
	for i := range vs {
		vs[i] = &tmproto.Validator{}
	}
	return vs
}

func TestSchemaForLightClientAttackEvidence_AcceptsAtCap(t *testing.T) {
	ev := &tmproto.LightClientAttackEvidence{ByzantineValidators: makeValidators(maxValidators)}
	require.NoError(t, protoutils.Scan[*tmproto.LightClientAttackEvidence](marshal(t, ev)))
}

func TestSchemaForLightClientAttackEvidence_RejectsOverCap(t *testing.T) {
	ev := &tmproto.LightClientAttackEvidence{ByzantineValidators: makeValidators(maxValidators + 1)}
	require.Error(t, protoutils.Scan[*tmproto.LightClientAttackEvidence](marshal(t, ev)))
}

func makeTxKeys(n int) []*tmproto.TxKey {
	ks := make([]*tmproto.TxKey, n)
	for i := range ks {
		ks[i] = &tmproto.TxKey{}
	}
	return ks
}

func TestSchemaForProposal_AcceptsTxKeysAtCap(t *testing.T) {
	proposal := &tmproto.Proposal{TxKeys: makeTxKeys(maxTxsPerBlock)}
	require.NoError(t, protoutils.Scan[*tmproto.Proposal](marshal(t, proposal)))
}

func TestSchemaForProposal_RejectsTxKeysOverCap(t *testing.T) {
	proposal := &tmproto.Proposal{TxKeys: makeTxKeys(maxTxsPerBlock + 1)}
	require.Error(t, protoutils.Scan[*tmproto.Proposal](marshal(t, proposal)))
}

package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard/wgtest"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func consensusAssembledBlock(lastCommit *tmproto.Commit, evidenceCommits ...*tmproto.Commit) *tmproto.Block {
	return &tmproto.Block{
		LastCommit: lastCommit,
		Evidence:   tmproto.EvidenceList{Evidence: wgtest.EvidenceList(evidenceCommits...)},
	}
}

func TestSchemaForBlock_AcceptsLastCommitAtCap(t *testing.T) {
	require.NoError(t, tmproto.SchemaForBlock.Scan(wgtest.Marshal(t,
		consensusAssembledBlock(wgtest.CommitWith(wgtest.MaxCommitSignatures)))))
}

func TestSchemaForBlock_RejectsLastCommitOverCap(t *testing.T) {
	require.Error(t, tmproto.SchemaForBlock.Scan(wgtest.Marshal(t,
		consensusAssembledBlock(wgtest.CommitWith(wgtest.MaxCommitSignatures+1)))))
}

func TestSchemaForBlock_RejectsEvidenceOverCap(t *testing.T) {
	require.Error(t, tmproto.SchemaForBlock.Scan(wgtest.Marshal(t,
		consensusAssembledBlock(nil, wgtest.CommitWith(wgtest.MaxCommitSignatures+1)))))
}

func TestSchemaForBlock_MultipleCommitsEachAtCapPass(t *testing.T) {
	// Each Commit is checked independently (per-instance). Two commits each
	// at exactly MaxCommitSignatures must both pass.
	require.NoError(t, tmproto.SchemaForBlock.Scan(wgtest.Marshal(t,
		consensusAssembledBlock(wgtest.CommitWith(wgtest.MaxCommitSignatures),
			wgtest.CommitWith(wgtest.MaxCommitSignatures)))))
}

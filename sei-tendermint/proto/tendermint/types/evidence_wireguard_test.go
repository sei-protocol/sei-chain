package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard/wgtest"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func lcaeEvidence(n int) *tmproto.Evidence {
	ev := wgtest.EvidenceWithCommit(wgtest.CommitWith(n))
	return &ev
}

func TestSchemaForEvidence_AcceptsAtCap(t *testing.T) {
	require.NoError(t, tmproto.SchemaForEvidence.Scan(wgtest.Marshal(t, lcaeEvidence(wgtest.MaxCommitSignatures))))
}

func TestSchemaForEvidence_RejectsOverCap(t *testing.T) {
	require.Error(t, tmproto.SchemaForEvidence.Scan(wgtest.Marshal(t, lcaeEvidence(wgtest.MaxCommitSignatures+1))))
}

func TestSchemaForEvidence_PassesDuplicateVoteEvidence(t *testing.T) {
	ev := &tmproto.Evidence{Sum: &tmproto.Evidence_DuplicateVoteEvidence{
		DuplicateVoteEvidence: &tmproto.DuplicateVoteEvidence{},
	}}
	require.NoError(t, tmproto.SchemaForEvidence.Scan(wgtest.Marshal(t, ev)))
}

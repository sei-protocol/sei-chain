package evidence_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evidence"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard/wgtest"
)

// TestWiring_EvidenceChannel asserts that the evidence channel descriptor
// installs a PreDecode hook that rejects an over-cap LightClientAttack
// payload for SchemaForEvidence.
func TestWiring_EvidenceChannel(t *testing.T) {
	pd, ok := evidence.GetChannelDescriptor().PreDecode.Get()
	require.True(t, ok, "evidence channel PreDecode is not set")
	ev := wgtest.EvidenceWithCommit(wgtest.CommitWith(wgtest.MaxCommitSignatures + 1))
	require.Error(t, pd(wgtest.Marshal(t, &ev)),
		"evidence channel PreDecode failed to reject an over-cap Commit signatures list")
}

package evidence_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard/wgtest"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// TestWiring_EvidenceChannel asserts that the evidence type implements
// WireguardScan and rejects an over-cap LightClientAttack payload.
func TestWiring_EvidenceChannel(t *testing.T) {
	ev := wgtest.EvidenceWithCommit(wgtest.CommitWith(wgtest.MaxCommitSignatures + 1))
	require.Error(t, ev.WireguardScan(wgtest.Marshal(t, &ev)),
		"Evidence.WireguardScan failed to reject an over-cap Commit signatures list")
}

// TestWiring_EvidenceAcceptsAtCap asserts that a payload at exactly the cap
// is accepted.
func TestWiring_EvidenceAcceptsAtCap(t *testing.T) {
	ev := wgtest.EvidenceWithCommit(wgtest.CommitWith(wgtest.MaxCommitSignatures))
	require.NoError(t, ev.WireguardScan(wgtest.Marshal(t, &ev)))
}

func init() {
	// Ensure Evidence implements WireguardScan at compile time.
	var _ interface{ WireguardScan([]byte) error } = (*tmproto.Evidence)(nil)
}

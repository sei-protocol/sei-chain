package evidence_test

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

// TestWiring_EvidenceChannel asserts that the evidence type has a
// wireguard registration and rejects an over-cap LightClientAttack payload.
func TestWiring_EvidenceChannel(t *testing.T) {
	ev := evidenceWithCommit(commitWith(maxCommitSignatures + 1))
	require.Error(t, protoutils.Scan[*tmproto.Evidence](marshal(t, &ev)),
		"protoutils.Scan[*types.Evidence] failed to reject an over-cap Commit signatures list")
}

// TestWiring_EvidenceAcceptsAtCap asserts that a payload at exactly the cap
// is accepted.
func TestWiring_EvidenceAcceptsAtCap(t *testing.T) {
	ev := evidenceWithCommit(commitWith(maxCommitSignatures))
	require.NoError(t, protoutils.Scan[*tmproto.Evidence](marshal(t, &ev)))
}

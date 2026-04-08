package consensus

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/stretchr/testify/require"
)

var testKey = ed25519.TestSecretKey([]byte("test"))

func makeSig(data string) crypto.Sig {
	return testKey.Sign([]byte(data))
}

func TestPeerStateMemoryLimits(t *testing.T) {

	peerID := types.NodeID("test-peer")

	testCases := []struct {
		name        string
		total       uint32
		expectError bool
	}{
		{"valid_total", 1, false},
		{"max_valid_total", types.MaxBlockPartsCount, false},
		{"excessive_total", types.MaxBlockPartsCount + 1, true},
		{"very_large_total", 4294967295, true},
	}

	// Test InitProposalBlockParts memory limits
	t.Run("InitProposalBlockParts", func(t *testing.T) {
		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				ps := NewPeerState(peerID)
				header := types.PartSetHeader{
					Total: tc.total,
					Hash:  make([]byte, 32),
				}
				ps.InitProposalBlockParts(header)
				if tc.expectError {
					require.Nil(t, ps.PRS.ProposalBlockParts, "Expected ProposalBlockParts to be nil for excessive Total")
				} else {
					require.NotNil(t, ps.PRS.ProposalBlockParts, "Expected ProposalBlockParts to be created")
					require.Equal(t, int(tc.total), ps.PRS.ProposalBlockParts.Size())
					require.Equal(t, header, ps.PRS.ProposalBlockPartSetHeader)
				}
			})
		}
	})
}

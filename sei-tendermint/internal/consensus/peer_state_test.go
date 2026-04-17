package consensus

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func peerStateSetup(h, r, v int) *PeerState {
	ps := NewPeerState("testPeerState")
	ps.PRS.Height = int64(h)
	ps.PRS.Round = int32(r)
	ps.ensureVoteBitArrays(int64(h), v)
	return ps
}

func TestSetHasVote(t *testing.T) {
	ps := peerStateSetup(1, 1, 1)
	pva := ps.PRS.Prevotes.Copy()

	// the peer giving an invalid index should returns ErrPeerStateInvalidVoteIndex
	v0 := &types.Vote{
		Height:         1,
		ValidatorIndex: -1,
		Round:          1,
		Type:           tmproto.PrevoteType,
	}

	if err := ps.SetHasVote(v0); !errors.Is(err, ErrPeerStateInvalidVoteIndex) {
		t.Fatalf("expected ErrPeerStateInvalidVoteIndex, got %v", err)
	}

	// the peer giving an invalid index should returns ErrPeerStateInvalidVoteIndex
	v1 := &types.Vote{
		Height:         1,
		ValidatorIndex: 1,
		Round:          1,
		Type:           tmproto.PrevoteType,
	}

	if err := ps.SetHasVote(v1); !errors.Is(err, ErrPeerStateInvalidVoteIndex) {
		t.Fatalf("expected ErrPeerStateInvalidVoteIndex, got %v", err)
	}

	// the peer giving a correct index should return nil (vote has been set)
	v2 := &types.Vote{
		Height:         1,
		ValidatorIndex: 0,
		Round:          1,
		Type:           tmproto.PrevoteType,
	}
	require.Nil(t, ps.SetHasVote(v2))

	// verify vote
	pva.SetIndex(0, true)
	require.Equal(t, pva, ps.getVoteBitArray(1, 1, tmproto.PrevoteType))

	// the vote is not in the correct height/round/voteType should return nil (ignore the vote)
	v3 := &types.Vote{
		Height:         2,
		ValidatorIndex: 0,
		Round:          1,
		Type:           tmproto.PrevoteType,
	}
	require.Nil(t, ps.SetHasVote(v3))
	// prevote bitarray has no update
	require.Equal(t, pva, ps.getVoteBitArray(1, 1, tmproto.PrevoteType))
}

func TestApplyHasVoteMessage(t *testing.T) {
	ps := peerStateSetup(1, 1, 1)
	pva := ps.PRS.Prevotes.Copy()

	// ignore the message with an invalid height
	msg := &HasVoteMessage{
		Height: 2,
	}
	require.Nil(t, ps.ApplyHasVoteMessage(msg))

	// apply a message like v2 in TestSetHasVote
	msg2 := &HasVoteMessage{
		Height: 1,
		Index:  0,
		Round:  1,
		Type:   tmproto.PrevoteType,
	}

	require.Nil(t, ps.ApplyHasVoteMessage(msg2))

	// verify vote
	pva.SetIndex(0, true)
	require.Equal(t, pva, ps.getVoteBitArray(1, 1, tmproto.PrevoteType))

	// skip test cases like v & v3 in TestSetHasVote due to the same path
}

func TestSetHasProposal(t *testing.T) {
	ps := peerStateSetup(1, 1, 1)

	// Test nil proposal - should be silently ignored
	ps.SetHasProposal(nil)
	require.False(t, ps.PRS.Proposal, "Nil proposal should be silently ignored")

	// Test invalid proposal (missing signature) - should be silently ignored
	invalidProposal := &types.Proposal{
		Type:     tmproto.ProposalType,
		Height:   1,
		Round:    1,
		POLRound: -1,
		BlockID: types.BlockID{
			Hash: make([]byte, crypto.HashSize),
			PartSetHeader: types.PartSetHeader{
				Total: 1,
				Hash:  make([]byte, crypto.HashSize),
			},
		},
		// Missing signature
	}
	ps.SetHasProposal(invalidProposal)
	require.True(t, ps.PRS.Proposal, "Valid structure proposal should be accepted regardless of signature")

	// Test valid proposal
	validProposal := &types.Proposal{
		Type:     tmproto.ProposalType,
		Height:   1,
		Round:    1,
		POLRound: -1,
		BlockID: types.BlockID{
			Hash: crypto.CRandBytes(crypto.HashSize),
			PartSetHeader: types.PartSetHeader{
				Total: 1,
				Hash:  crypto.CRandBytes(crypto.HashSize),
			},
		},
		Signature: makeSig("signature"),
	}
	ps.SetHasProposal(validProposal)
	require.True(t, ps.PRS.Proposal, "Valid proposal should be accepted")

	// Test proposal for different height/round - should be silently ignored
	ps2 := peerStateSetup(2, 1, 1) // Different peer state with height 2
	differentProposal := &types.Proposal{
		Type:     tmproto.ProposalType,
		Height:   2, // Different height
		Round:    1,
		POLRound: -1,
		BlockID: types.BlockID{
			Hash: crypto.CRandBytes(crypto.HashSize),
			PartSetHeader: types.PartSetHeader{
				Total: 1,
				Hash:  crypto.CRandBytes(crypto.HashSize),
			},
		},
		Signature: makeSig("signature"),
	}
	ps2.SetHasProposal(differentProposal)
	require.True(t, ps2.PRS.Proposal, "Proposal with matching height should be accepted")
}

func TestInitProposalBlockPartsMemoryLimit(t *testing.T) {

	peerID := types.NodeID("test-peer")
	ps := NewPeerState(peerID)

	testCases := []struct {
		name           string
		total          uint32
		expectBitArray bool
	}{
		{"valid small total", 1, true},
		{"max valid total", types.MaxBlockPartsCount, true},
		{"over max limit", types.MaxBlockPartsCount + 1, false},
		{"large total value", 4294967295, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Reset peer state for each test
			ps = NewPeerState(peerID)

			header := types.PartSetHeader{
				Total: tc.total,
				Hash:  []byte("test-hash"),
			}

			ps.InitProposalBlockParts(header)

			if tc.expectBitArray {
				require.NotNil(t, ps.PRS.ProposalBlockParts, "Expected ProposalBlockParts to be created")
				require.Equal(t, int(tc.total), ps.PRS.ProposalBlockParts.Size())
				require.Equal(t, header, ps.PRS.ProposalBlockPartSetHeader)
			} else {
				require.Nil(t, ps.PRS.ProposalBlockParts, "Expected ProposalBlockParts to be nil for excessive Total")
			}
		})
	}
}

func TestInitProposalBlockPartsAlreadySet(t *testing.T) {

	peerID := types.NodeID("test-peer")
	ps := NewPeerState(peerID)

	// Set up initial proposal block parts
	initialHeader := types.PartSetHeader{
		Total: 5,
		Hash:  []byte("initial-hash"),
	}
	ps.InitProposalBlockParts(initialHeader)
	require.NotNil(t, ps.PRS.ProposalBlockParts)
	require.Equal(t, 5, ps.PRS.ProposalBlockParts.Size())

	// Try to set again with different header - should be ignored
	newHeader := types.PartSetHeader{
		Total: 10,
		Hash:  []byte("new-hash"),
	}
	ps.InitProposalBlockParts(newHeader)

	// Should still have the original values
	require.NotNil(t, ps.PRS.ProposalBlockParts)
	require.Equal(t, 5, ps.PRS.ProposalBlockParts.Size())
	require.Equal(t, initialHeader, ps.PRS.ProposalBlockPartSetHeader)
}

func TestSetHasProposalEdgeCases(t *testing.T) {

	peerID := types.NodeID("test-peer")

	testCases := []struct {
		name           string
		setupPeerState func(ps *PeerState)
		proposal       *types.Proposal
		expectProposal bool
		expectPanic    bool
	}{
		{
			name: "wrong height - should ignore",
			setupPeerState: func(ps *PeerState) {
				ps.PRS.Height = 1
				ps.PRS.Round = 0
			},
			proposal: &types.Proposal{
				Type:     tmproto.ProposalType,
				Height:   2, // Wrong height
				Round:    0,
				POLRound: -1,
				BlockID: types.BlockID{
					Hash: make([]byte, 32),
					PartSetHeader: types.PartSetHeader{
						Total: 1,
						Hash:  make([]byte, 32),
					},
				},
				Timestamp: time.Now(),
				Signature: makeSig("test-signature"),
			},
			expectProposal: false,
			expectPanic:    false,
		},
		{
			name: "already has proposal - should remain unchanged",
			setupPeerState: func(ps *PeerState) {
				ps.PRS.Height = 1
				ps.PRS.Round = 0
				ps.PRS.Proposal = true // Already has proposal
			},
			proposal: &types.Proposal{
				Type:     tmproto.ProposalType,
				Height:   1,
				Round:    0,
				POLRound: -1,
				BlockID: types.BlockID{
					Hash: make([]byte, 32),
					PartSetHeader: types.PartSetHeader{
						Total: 1,
						Hash:  make([]byte, 32),
					},
				},
				Timestamp: time.Now(),
				Signature: makeSig("test-signature"),
			},
			expectProposal: true, // Should remain true
			expectPanic:    false,
		},
		{
			name: "valid proposal - should be accepted",
			setupPeerState: func(ps *PeerState) {
				ps.PRS.Height = 1
				ps.PRS.Round = 0
			},
			proposal: &types.Proposal{
				Type:     tmproto.ProposalType,
				Height:   1,
				Round:    0,
				POLRound: -1,
				BlockID: types.BlockID{
					Hash: make([]byte, 32),
					PartSetHeader: types.PartSetHeader{
						Total: 1, // Valid
						Hash:  make([]byte, 32),
					},
				},
				Timestamp: time.Now(),
				Signature: makeSig("test-signature"),
			},
			expectProposal: true, // Should be set
			expectPanic:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ps := NewPeerState(peerID)
			tc.setupPeerState(ps)

			if tc.expectPanic {
				require.Panics(t, func() {
					ps.SetHasProposal(tc.proposal)
				})
				return
			}

			// SetHasProposal doesn't return error - it handles issues silently
			ps.SetHasProposal(tc.proposal)
			require.Equal(t, tc.expectProposal, ps.PRS.Proposal)
		})
	}
}

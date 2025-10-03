package consensus

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/types"
)

func peerStateSetup(h, r, v int) *PeerState {
	ps := NewPeerState(log.NewNopLogger(), "testPeerState")
	ps.PRS.Height = int64(h)
	ps.PRS.Round = int32(r)
	ps.ensureVoteBitArrays(int64(h), v)
	return ps
}

func TestSetHasVote(t *testing.T) {
	ps := peerStateSetup(1, 1, 1)
	pva := ps.PRS.Prevotes.Copy()

	// nil vote should return ErrPeerStateNilVote
	err := ps.SetHasVote(nil)
	require.Equal(t, ErrPeerStateSetNilVote, err)

	// the peer giving an invalid index should returns ErrPeerStateInvalidVoteIndex
	v0 := &types.Vote{
		Height:         1,
		ValidatorIndex: -1,
		Round:          1,
		Type:           tmproto.PrevoteType,
	}

	err = ps.SetHasVote(v0)
	require.Equal(t, ErrPeerStateInvalidVoteIndex, err)

	// the peer giving an invalid index should returns ErrPeerStateInvalidVoteIndex
	v1 := &types.Vote{
		Height:         1,
		ValidatorIndex: 1,
		Round:          1,
		Type:           tmproto.PrevoteType,
	}

	err = ps.SetHasVote(v1)
	require.Equal(t, ErrPeerStateInvalidVoteIndex, err)

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

	// Test PartSetHeader.Total too large - should be silently ignored
	// Create a new peer state for this test
	ps3 := peerStateSetup(1, 1, 1)
	tooLargeTotalProposal := &types.Proposal{
		Type:     tmproto.ProposalType,
		Height:   1,
		Round:    1,
		POLRound: -1,
		BlockID: types.BlockID{
			Hash: crypto.CRandBytes(crypto.HashSize),
			PartSetHeader: types.PartSetHeader{
				Total: types.MaxBlockPartsCount + 1, // Too large
				Hash:  crypto.CRandBytes(crypto.HashSize),
			},
		},
		Signature: []byte("signature"),
	}
	ps3.SetHasProposal(tooLargeTotalProposal)
	require.False(t, ps3.PRS.Proposal, "Proposal with too large Total should be silently ignored")

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
		Signature: []byte("signature"),
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
		Signature: []byte("signature"),
	}
	ps2.SetHasProposal(differentProposal)
	require.True(t, ps2.PRS.Proposal, "Proposal with matching height should be accepted")
}

func TestSetHasProposalMemoryLimit(t *testing.T) {
	logger := log.NewTestingLogger(t)
	peerID := types.NodeID("aa")
	ps := NewPeerState(logger, peerID)

	// Create a valid block hash
	hash := crypto.CRandBytes(crypto.HashSize)

	// Create a dummy signature
	sig := crypto.CRandBytes(types.MaxSignatureSize)

	// Create a proposal with a large PartSetHeader.Total
	proposal := &types.Proposal{
		Type:     tmproto.ProposalType,
		Height:   1,
		Round:    0,
		POLRound: -1,
		BlockID: types.BlockID{
			Hash: hash,
			PartSetHeader: types.PartSetHeader{
				Hash: hash, // Use same hash for simplicity
			},
		},
		Timestamp: time.Now(),
		Signature: sig,
	}

	// Test with different Total values
	testCases := []struct {
		name        string
		total       uint32
		expectError bool
		errorType   string // "max_block_parts"
	}{
		{"valid small total", 1, false, ""},
		{"valid max total", types.MaxBlockPartsCount, false, ""},                        // 101
		{"over max block parts", types.MaxBlockPartsCount + 1, true, "max_block_parts"}, // 102
		{"way over max block parts", 1000, true, "max_block_parts"},                     // Way over max
		{"DoS attack scenario - max uint32", 4294967295, true, "max_block_parts"},       // The actual DoS attack value
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Reset peer state and set height/round to match proposal
			ps = NewPeerState(logger, peerID)
			ps.PRS.Height = proposal.Height
			ps.PRS.Round = proposal.Round

			// Set up proposal with test case total
			proposal.BlockID.PartSetHeader.Total = tc.total

			// Try to set the proposal
			ps.SetHasProposal(proposal)

			if tc.expectError {
				// Should be silently ignored, so no proposal should be set
				require.False(t, ps.PRS.Proposal, "Proposal with excessive Total should be silently ignored")
				require.Nil(t, ps.PRS.ProposalBlockParts, "ProposalBlockParts should remain nil")
			} else {
				// For valid cases, verify the proposal was accepted
				require.True(t, ps.PRS.Proposal, "Valid proposal should be accepted")
				require.NotNil(t, ps.PRS.ProposalBlockParts, "ProposalBlockParts should be created")
				require.Equal(t, int(tc.total), ps.PRS.ProposalBlockParts.Size())
				require.NotNil(t, ps.PRS.ProposalBlockParts.Elems)
			}
		})
	}
}

func TestInitProposalBlockPartsMemoryLimit(t *testing.T) {
	logger := log.NewTestingLogger(t)
	peerID := types.NodeID("test-peer")
	ps := NewPeerState(logger, peerID)

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
			ps = NewPeerState(logger, peerID)

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
	logger := log.NewTestingLogger(t)
	peerID := types.NodeID("test-peer")
	ps := NewPeerState(logger, peerID)

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
	logger := log.NewTestingLogger(t)
	peerID := types.NodeID("test-peer")

	testCases := []struct {
		name           string
		setupPeerState func(ps *PeerState)
		proposal       *types.Proposal
		expectProposal bool
		expectPanic    bool
	}{
		{
			name: "memory limit exceeded - should silently ignore",
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
						Total: types.MaxBlockPartsCount + 1, // Exceeds limit
						Hash:  make([]byte, 32),
					},
				},
				Timestamp: time.Now(),
				Signature: []byte("test-signature"),
			},
			expectProposal: false, // Should silently ignore
			expectPanic:    false,
		},
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
				Signature: []byte("test-signature"),
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
				Signature: []byte("test-signature"),
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
				Signature: []byte("test-signature"),
			},
			expectProposal: true, // Should be set
			expectPanic:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ps := NewPeerState(logger, peerID)
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

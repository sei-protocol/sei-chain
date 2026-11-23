package gov

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpeditedTextKeepsItsContentWhenDemoted(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.open(expedProposal, usei(40))
	h.time = startTime + testParams.ExpeditedVotingPeriod
	h.endBlock(0)
	got := h.proposal(id)
	require.False(t, got.IsExpedited, "the proposal is now regular")
	content, err := decodeContent(got.ContentType, got.Content)
	require.NoError(t, err)
	require.Equal(t, textContent{Title: "t", Description: "d", IsExpedited: true}, content, "its content is unchanged, as in V2")
}

func TestProposalReturnsStoredContent(t *testing.T) {
	h := newHarness(t)
	got := h.proposal(h.submit(alice, textProposal, nil))
	wantType, wantBytes := encodeContent(textContent{Title: "t", Description: "d"})
	require.Equal(t, wantType, got.ContentType)
	require.Equal(t, wantBytes, got.Content)
}

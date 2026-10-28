package gov

import (
	"errors"
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
	require.Contains(t, string(got.Content), `"is_expedited":true`, "its content is unchanged, as in V2")
}

func TestSubmitRejectsFieldsGovDoesNotRead(t *testing.T) {
	h := newHarness(t)
	for name, content := range map[string]string{
		"unknown field":          `{"title":"t","description":"d","type":"Text","deposit":"10usei"}`,
		"unknown plan field":     `{"title":"t","description":"d","type":"SoftwareUpgrade","plan":{"name":"v2","height":10,"time":"2030-01-01T00:00:00Z"}}`,
		"miscased field":         `{"Title":"t","description":"d","type":"Text"}`,
		"miscased plan field":    `{"title":"t","description":"d","type":"SoftwareUpgrade","plan":{"NAME":"v2","height":10}}`,
		"duplicate field":        `{"title":"t","title":"u","description":"d","type":"Text"}`,
		"duplicate by case":      `{"title":"t","TITLE":"u","description":"d","type":"Text"}`,
		"duplicate plan field":   `{"title":"t","description":"d","type":"SoftwareUpgrade","plan":{"name":"v2","name":"v3","height":10}}`,
		"miscased changes":       `{"title":"t","description":"d","type":"ParameterChange","Changes":[{"subspace":"gov","key":"votingparams","value":{"voting_period":"1000000000"}}]}`,
		"unknown change field":   `{"title":"t","description":"d","type":"ParameterChange","changes":[{"subspace":"gov","key":"votingparams","value":{"voting_period":"1000000000"},"note":"x"}]}`,
		"duplicate change field": `{"title":"t","description":"d","type":"ParameterChange","changes":[{"subspace":"gov","key":"votingparams","key":"tallyparams","value":{"voting_period":"1000000000"}}]}`,
		"null plan on text":      `{"title":"t","description":"d","type":"Text","plan":null}`,
		"null plan on cancel":    `{"title":"t","description":"d","type":"CancelSoftwareUpgrade","plan":null}`,
		"null changes on text":   `{"title":"t","description":"d","type":"Text","changes":null}`,
		"V2 community pool type": `{"title":"t","description":"d","type":"Text","community_pool_spend":{"recipient":"0x1","amount":"1usei"}}`,
	} {
		_, err := h.call(alice, callOpts{}, SubmitProposalMethod, content)
		require.True(t, errors.Is(err, errUnknownField) || errors.Is(err, errDuplicateField), "%s: %v", name, err)
	}
	require.Zero(t, newStore(h.state).lastProposalID())
}

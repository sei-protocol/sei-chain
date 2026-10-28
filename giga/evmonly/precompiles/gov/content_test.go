package gov

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
)

// awkward is text that JSON escapes: HTML characters, a quote and backslash,
// non-ASCII, a line separator, control characters and invalid UTF-8.
const awkward = "a<b>&c\"q\\ é 😀   \x01 \x7f \t\n \xff"

// TestContentMatchesV2 checks proposal().content against V2's encoding of the
// Content its submission creates.
func TestContentMatchesV2(t *testing.T) {
	reg := codectypes.NewInterfaceRegistry()
	govtypes.RegisterInterfaces(reg)
	upgradetypes.RegisterInterfaces(reg)
	cdc := codec.NewProtoCodec(reg)
	plan := upgradetypes.Plan{Name: awkward, Height: 1_000, Info: `{"binaries":{"linux/amd64":"https://x?a=1&b=2"}}`}
	for name, tc := range map[string]struct {
		submitted map[string]any
		v2        govtypes.Content
	}{
		"text": {
			map[string]any{"title": awkward, "description": "d", "type": TypeText},
			govtypes.NewTextProposal(awkward, "d", false),
		},
		"expedited text": {
			map[string]any{"title": "t", "description": awkward, "type": TypeText, "is_expedited": true},
			govtypes.NewTextProposal("t", awkward, true),
		},
		"software upgrade": {
			map[string]any{"title": "t", "description": "d", "type": TypeSoftwareUpgrade, "plan": map[string]any{"name": plan.Name, "height": plan.Height, "info": plan.Info}},
			upgradetypes.NewSoftwareUpgradeProposal("t", "d", plan),
		},
		"software upgrade without info": {
			map[string]any{"title": "t", "description": "d", "type": TypeSoftwareUpgrade, "plan": map[string]any{"name": "v2", "height": 10}},
			upgradetypes.NewSoftwareUpgradeProposal("t", "d", upgradetypes.Plan{Name: "v2", Height: 10}),
		},
		"cancel software upgrade": {
			map[string]any{"title": "t", "description": "d", "type": TypeCancelSoftwareUpgrade, "is_expedited": true},
			upgradetypes.NewCancelSoftwareUpgradeProposal("t", "d"),
		},
	} {
		submitted, err := json.Marshal(tc.submitted)
		require.NoError(t, err, name)
		h := newHarness(t)
		id := h.submit(alice, string(submitted), nil)

		v2, err := codectypes.NewAnyWithValue(tc.v2.(proto.Message))
		require.NoError(t, err, name)
		want, err := cdc.MarshalAsJSON(v2)
		require.NoError(t, err, name)
		require.Equal(t, string(want), string(h.proposal(id).Content), name)
	}
}

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
		"V2 community pool type": `{"title":"t","description":"d","type":"Text","community_pool_spend":{"recipient":"0x1","amount":"1usei"}}`,
	} {
		_, err := h.call(alice, callOpts{}, SubmitProposalMethod, content)
		require.True(t, errors.Is(err, errUnknownField) || errors.Is(err, errDuplicateField), "%s: %v", name, err)
	}
	require.Zero(t, newStore(h.state).lastProposalID())
}

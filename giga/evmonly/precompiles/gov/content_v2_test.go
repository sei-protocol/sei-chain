package gov_test

import (
	"encoding/json"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
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
			map[string]any{"title": awkward, "description": "d", "type": gov.TypeText},
			govtypes.NewTextProposal(awkward, "d", false),
		},
		"expedited text": {
			map[string]any{"title": "t", "description": awkward, "type": gov.TypeText, "is_expedited": true},
			govtypes.NewTextProposal("t", awkward, true),
		},
		"software upgrade": {
			map[string]any{"title": "t", "description": "d", "type": gov.TypeSoftwareUpgrade, "plan": map[string]any{"name": plan.Name, "height": plan.Height, "info": plan.Info}},
			upgradetypes.NewSoftwareUpgradeProposal("t", "d", plan),
		},
		"software upgrade without info": {
			map[string]any{"title": "t", "description": "d", "type": gov.TypeSoftwareUpgrade, "plan": map[string]any{"name": "v2", "height": 10}},
			upgradetypes.NewSoftwareUpgradeProposal("t", "d", upgradetypes.Plan{Name: "v2", Height: 10}),
		},
		"cancel software upgrade": {
			map[string]any{"title": "t", "description": "d", "type": gov.TypeCancelSoftwareUpgrade, "is_expedited": true},
			upgradetypes.NewCancelSoftwareUpgradeProposal("t", "d"),
		},
	} {
		submitted, err := json.Marshal(tc.submitted)
		require.NoError(t, err, name)

		v2, err := codectypes.NewAnyWithValue(tc.v2.(proto.Message))
		require.NoError(t, err, name)
		want, err := cdc.MarshalAsJSON(v2)
		require.NoError(t, err, name)
		require.Equal(t, string(want), string(gov.SubmittedContent(t, string(submitted))), name)
	}
}

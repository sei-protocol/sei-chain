package cli

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParamChangesRoundTrip(t *testing.T) {
	changes, err := flatChanges([]v2ParamChange{
		{Subspace: "gov", Key: "tallyparams", Value: json.RawMessage(`{"veto_threshold":"0.334","quorum":0.4}`)},
		{Subspace: "gov", Key: "depositparams", Value: json.RawMessage(`{"min_deposit":"10usei","max_deposit_period":"172800000000000"}`)},
	})
	require.NoError(t, err)
	v2, err := v2ParamChanges(changes)
	require.NoError(t, err)
	require.Len(t, v2, 2)
	require.Equal(t, "tallyparams", v2[0].Key)
	require.JSONEq(t, `{"quorum":"0.400000000000000000","veto_threshold":"0.334000000000000000"}`, v2[0].Value)
	require.Equal(t, "depositparams", v2[1].Key)
	require.JSONEq(t, `{"max_deposit_period":"172800000000000","min_deposit":[{"denom":"usei","amount":"10"}]}`, v2[1].Value)
}

func TestFlatChangesRejects(t *testing.T) {
	for name, c := range map[string]struct {
		changes []v2ParamChange
		want    string
	}{
		"repeated param": {[]v2ParamChange{
			{Subspace: "gov", Key: "tallyparams", Value: json.RawMessage(`{"quorum":"0.4"}`)},
			{Subspace: "gov", Key: "tallyparams.quorum", Value: json.RawMessage(`"0.5"`)},
		}, "changed more than once"},
		"unknown field":     {[]v2ParamChange{{Subspace: "gov", Key: "tallyparams", Value: json.RawMessage(`{"quorom":"0.4"}`)}}, "gov/tallyparams.quorom is not a param"},
		"scalar for struct": {[]v2ParamChange{{Subspace: "gov", Key: "tallyparams", Value: json.RawMessage(`"0.4"`)}}, "value must be an object"},
		"negative dec":      {[]v2ParamChange{{Subspace: "gov", Key: "tallyparams.quorum", Value: json.RawMessage(`"-0.4"`)}}, "out of range"},
		"bad duration":      {[]v2ParamChange{{Subspace: "gov", Key: "votingparams.voting_period", Value: json.RawMessage(`"soon"`)}}, "want nanoseconds"},
		"other denom":       {[]v2ParamChange{{Subspace: "gov", Key: "depositparams.min_deposit", Value: json.RawMessage(`"10uatom"`)}}, "only usei"},
		"none":              {nil, "no param changes"},
	} {
		_, err := flatChanges(c.changes)
		require.ErrorContains(t, err, c.want, name)
	}
}

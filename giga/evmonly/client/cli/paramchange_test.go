package cli

import (
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common"
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

func TestStakingParamChangesRoundTrip(t *testing.T) {
	changes, err := flatChanges([]v2ParamChange{
		{Subspace: "staking", Key: "MaxValidators", Value: json.RawMessage(`40`)},
		{Subspace: "staking", Key: "MaxVotingPowerEnforcementThreshold", Value: json.RawMessage(`"2000000"`)},
		{Subspace: "staking", Key: "MaxVotingPower", Value: json.RawMessage(`"0.25"`)},
	})
	require.NoError(t, err)
	v2, err := v2ParamChanges(changes)
	require.NoError(t, err)
	require.Len(t, v2, 3)
	require.JSONEq(t, `40`, v2[0].Value)
	require.JSONEq(t, `"2000000"`, v2[1].Value)
	require.JSONEq(t, `"0.250000000000000000"`, v2[2].Value)
}

func TestDistributionParamChangesRoundTrip(t *testing.T) {
	changes, err := flatChanges([]v2ParamChange{
		{Subspace: "distribution", Key: "withdrawaddrenabled", Value: json.RawMessage(`false`)},
		{Subspace: "distribution", Key: "daoaddress", Value: json.RawMessage(`"0x00000000000000000000000000000000000da0da"`)},
	})
	require.NoError(t, err)
	v2, err := v2ParamChanges(changes)
	require.NoError(t, err)
	require.Len(t, v2, 2)
	require.JSONEq(t, `false`, v2[0].Value)
	require.JSONEq(t, `"`+common.HexToAddress("0xda0da").Hex()+`"`, v2[1].Value, "a checksummed address")
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
		"unknown field":      {[]v2ParamChange{{Subspace: "gov", Key: "tallyparams", Value: json.RawMessage(`{"quorom":"0.4"}`)}}, "gov/tallyparams.quorom is not a param"},
		"scalar for struct":  {[]v2ParamChange{{Subspace: "gov", Key: "tallyparams", Value: json.RawMessage(`"0.4"`)}}, "value must be an object"},
		"negative dec":       {[]v2ParamChange{{Subspace: "gov", Key: "tallyparams.quorum", Value: json.RawMessage(`"-0.4"`)}}, "out of range"},
		"bad duration":       {[]v2ParamChange{{Subspace: "gov", Key: "votingparams.voting_period", Value: json.RawMessage(`"soon"`)}}, "want nanoseconds"},
		"other denom":        {[]v2ParamChange{{Subspace: "gov", Key: "depositparams.min_deposit", Value: json.RawMessage(`"10uatom"`)}}, "only usei"},
		"blank coins":        {[]v2ParamChange{{Subspace: "gov", Key: "depositparams.min_deposit", Value: json.RawMessage(`""`)}}, "want a coin amount"},
		"null coins":         {[]v2ParamChange{{Subspace: "gov", Key: "depositparams.min_deposit", Value: json.RawMessage(`null`)}}, "want a coin amount"},
		"fractional int":     {[]v2ParamChange{{Subspace: "staking", Key: "MaxValidators", Value: json.RawMessage(`"1.5"`)}}, "want a whole number"},
		"negative int":       {[]v2ParamChange{{Subspace: "staking", Key: "MaxEntries", Value: json.RawMessage(`-1`)}}, "out of range"},
		"string bool":        {[]v2ParamChange{{Subspace: "distribution", Key: "withdrawaddrenabled", Value: json.RawMessage(`"true"`)}}, "want true or false"},
		"short address":      {[]v2ParamChange{{Subspace: "distribution", Key: "daoaddress", Value: json.RawMessage(`"0xda0da"`)}}, "20-byte hex address"},
		"unprefixed address": {[]v2ParamChange{{Subspace: "distribution", Key: "daoaddress", Value: json.RawMessage(`"00000000000000000000000000000000000da0da"`)}}, "0x-prefixed"},
		"none":               {nil, "no param changes"},
	} {
		_, err := flatChanges(c.changes)
		require.ErrorContains(t, err, c.want, name)
	}
}

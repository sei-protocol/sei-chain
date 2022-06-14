package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseDenomTrace(t *testing.T) {
	testCases := []struct {
		name     string
		denom    string
		expTrace DenomTrace
	}{
		{"empty denom", "", DenomTrace{}},
		{"base denom", "uatom", DenomTrace{BaseDenom: "uatom"}},
		{"base denom ending with '/'", "uatom/", DenomTrace{BaseDenom: "uatom/"}},
		{"base denom with single '/'s", "gamm/pool/1", DenomTrace{BaseDenom: "gamm/pool/1"}},
		{"base denom with double '/'s", "gamm//pool//1", DenomTrace{BaseDenom: "gamm//pool//1"}},
		{"trace info", "transfer/channelToA/uatom", DenomTrace{BaseDenom: "uatom", Path: "transfer/channelToA"}},
		{"trace info with base denom ending in '/'", "transfer/channelToA/uatom/", DenomTrace{BaseDenom: "uatom/", Path: "transfer/channelToA"}},
		{"trace info with single '/' in base denom", "transfer/channelToA/erc20/0x85bcBCd7e79Ec36f4fBBDc54F90C643d921151AA", DenomTrace{BaseDenom: "erc20/0x85bcBCd7e79Ec36f4fBBDc54F90C643d921151AA", Path: "transfer/channelToA"}},
		{"trace info with multiple '/'s in base denom", "transfer/channelToA/gamm/pool/1", DenomTrace{BaseDenom: "gamm/pool/1", Path: "transfer/channelToA"}},
		{"trace info with multiple double '/'s in base denom", "transfer/channelToA/gamm//pool//1", DenomTrace{BaseDenom: "gamm//pool//1", Path: "transfer/channelToA"}},
		{"trace info with multiple port/channel pairs", "transfer/channelToA/transfer/channelToB/uatom", DenomTrace{BaseDenom: "uatom", Path: "transfer/channelToA/transfer/channelToB"}},
		{"incomplete path", "transfer/uatom", DenomTrace{BaseDenom: "transfer/uatom"}},
		{"invalid path (1)", "transfer//uatom", DenomTrace{BaseDenom: "uatom", Path: "transfer/"}},
		{"invalid path (2)", "channelToA/transfer/uatom", DenomTrace{BaseDenom: "channelToA/transfer/uatom"}},
		{"invalid path (3)", "uatom/transfer", DenomTrace{BaseDenom: "uatom/transfer"}},
		{"invalid path (4)", "transfer/channelToA", DenomTrace{BaseDenom: "transfer/channelToA"}},
		{"invalid path (5)", "transfer/channelToA/", DenomTrace{Path: "transfer/channelToA"}},
		{"invalid path (6)", "transfer/channelToA/transfer", DenomTrace{BaseDenom: "transfer", Path: "transfer/channelToA"}},
		{"invalid path (7)", "transfer/channelToA/transfer/channelToB", DenomTrace{Path: "transfer/channelToA/transfer/channelToB"}},
	}

	for _, tc := range testCases {
		trace := ParseDenomTrace(tc.denom)
		require.Equal(t, tc.expTrace, trace, tc.name)
	}
}

func TestDenomTrace_IBCDenom(t *testing.T) {
	testCases := []struct {
		name     string
		trace    DenomTrace
		expDenom string
	}{
		{"base denom", DenomTrace{BaseDenom: "uatom"}, "uatom"},
		{"trace info", DenomTrace{BaseDenom: "uatom", Path: "transfer/channelToA"}, "ibc/7F1D3FCF4AE79E1554D670D1AD949A9BA4E4A3C76C63093E17E446A46061A7A2"},
	}

	for _, tc := range testCases {
		denom := tc.trace.IBCDenom()
		require.Equal(t, tc.expDenom, denom, tc.name)
	}
}

func TestDenomTrace_Validate(t *testing.T) {
	testCases := []struct {
		name     string
		trace    DenomTrace
		expError bool
	}{
		{"base denom only", DenomTrace{BaseDenom: "uatom"}, false},
		{"base denom only with single '/'", DenomTrace{BaseDenom: "erc20/0x85bcBCd7e79Ec36f4fBBDc54F90C643d921151AA"}, false},
		{"base denom only with multiple '/'s", DenomTrace{BaseDenom: "gamm/pool/1"}, false},
		{"empty DenomTrace", DenomTrace{}, true},
		{"valid single trace info", DenomTrace{BaseDenom: "uatom", Path: "transfer/channelToA"}, false},
		{"valid multiple trace info", DenomTrace{BaseDenom: "uatom", Path: "transfer/channelToA/transfer/channelToB"}, false},
		{"single trace identifier", DenomTrace{BaseDenom: "uatom", Path: "transfer"}, true},
		{"invalid port ID", DenomTrace{BaseDenom: "uatom", Path: "(transfer)/channelToA"}, true},
		{"invalid channel ID", DenomTrace{BaseDenom: "uatom", Path: "transfer/(channelToA)"}, true},
		{"empty base denom with trace", DenomTrace{BaseDenom: "", Path: "transfer/channelToA"}, true},
	}

	for _, tc := range testCases {
		err := tc.trace.Validate()
		if tc.expError {
			require.Error(t, err, tc.name)
			continue
		}
		require.NoError(t, err, tc.name)
	}
}

func TestTraces_Validate(t *testing.T) {
	testCases := []struct {
		name     string
		traces   Traces
		expError bool
	}{
		{"empty Traces", Traces{}, false},
		{"valid multiple trace info", Traces{{BaseDenom: "uatom", Path: "transfer/channelToA/transfer/channelToB"}}, false},
		{
			"valid multiple trace info",
			Traces{
				{BaseDenom: "uatom", Path: "transfer/channelToA/transfer/channelToB"},
				{BaseDenom: "uatom", Path: "transfer/channelToA/transfer/channelToB"},
			},
			true,
		},
		{"empty base denom with trace", Traces{{BaseDenom: "", Path: "transfer/channelToA"}}, true},
	}

	for _, tc := range testCases {
		err := tc.traces.Validate()
		if tc.expError {
			require.Error(t, err, tc.name)
			continue
		}
		require.NoError(t, err, tc.name)
	}
}

func TestValidatePrefixedDenom(t *testing.T) {
	testCases := []struct {
		name     string
		denom    string
		expError bool
	}{
		{"prefixed denom", "transfer/channelToA/uatom", false},
		{"prefixed denom with '/'", "transfer/channelToA/gamm/pool/1", false},
		{"empty prefix", "/uatom", false},
		{"empty identifiers", "//uatom", false},
		{"base denom", "uatom", false},
		{"base denom with single '/'", "erc20/0x85bcBCd7e79Ec36f4fBBDc54F90C643d921151AA", false},
		{"base denom with multiple '/'s", "gamm/pool/1", false},
		{"invalid port ID", "(transfer)/channelToA/uatom", false},
		{"empty denom", "", true},
		{"single trace identifier", "transfer/", true},
		{"invalid channel ID", "transfer/(channelToA)/uatom", true},
	}

	for _, tc := range testCases {
		err := ValidatePrefixedDenom(tc.denom)
		if tc.expError {
			require.Error(t, err, tc.name)
			continue
		}
		require.NoError(t, err, tc.name)
	}
}

func TestValidateIBCDenom(t *testing.T) {
	testCases := []struct {
		name     string
		denom    string
		expError bool
	}{
		{"denom with trace hash", "ibc/7F1D3FCF4AE79E1554D670D1AD949A9BA4E4A3C76C63093E17E446A46061A7A2", false},
		{"base denom", "uatom", false},
		{"base denom ending with '/'", "uatom/", false},
		{"base denom with single '/'s", "gamm/pool/1", false},
		{"base denom with double '/'s", "gamm//pool//1", false},
		{"non-ibc prefix with hash", "notibc/7F1D3FCF4AE79E1554D670D1AD949A9BA4E4A3C76C63093E17E446A46061A7A2", false},
		{"empty denom", "", true},
		{"denom 'ibc'", "ibc", true},
		{"denom 'ibc/'", "ibc/", true},
		{"invald hash", "ibc/!@#$!@#", true},
	}

	for _, tc := range testCases {
		err := ValidateIBCDenom(tc.denom)
		if tc.expError {
			require.Error(t, err, tc.name)
			continue
		}
		require.NoError(t, err, tc.name)
	}
}

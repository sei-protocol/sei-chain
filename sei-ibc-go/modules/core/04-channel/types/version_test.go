package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
)

func TestSplitVersions(t *testing.T) {
	testCases := []struct {
		name       string
		version    string
		mwVersion  string
		appVersion string
	}{
		{
			"single wrapped middleware",
			"fee29-1:ics20-1",
			"fee29-1",
			"ics20-1",
		},
		{
			"multiple wrapped middleware",
			"fee29-1:whitelist:ics20-1",
			"fee29-1",
			"whitelist:ics20-1",
		},
		{
			"no middleware",
			"ics20-1",
			"",
			"ics20-1",
		},
	}

	for _, tc := range testCases {
		mwVersion, appVersion := types.SplitChannelVersion(tc.version)
		require.Equal(t, tc.mwVersion, mwVersion, "middleware version is unexpected for case: %s", tc.name)
		require.Equal(t, tc.appVersion, appVersion, "app version is unexpected for case: %s", tc.name)
	}
}

func TestMergeVersions(t *testing.T) {
	testCases := []struct {
		name     string
		versions []string
		merged   string
	}{
		{
			"single version",
			[]string{"ics20-1"},
			"ics20-1",
		},
		{
			"empty version",
			[]string{},
			"",
		},
		{
			"two versions",
			[]string{"fee29-1", "ics20-1"},
			"fee29-1:ics20-1",
		},
		{
			"multiple versions",
			[]string{"fee29-1", "whitelist", "ics20-1"},
			"fee29-1:whitelist:ics20-1",
		},
	}

	for _, tc := range testCases {
		actual := types.MergeChannelVersions(tc.versions...)
		require.Equal(t, tc.merged, actual, "merged versions string does not equal expected value")
	}
}

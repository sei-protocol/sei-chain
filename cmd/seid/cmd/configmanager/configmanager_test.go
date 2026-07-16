package configmanager

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSelect covers the dispatch table: unset and "legacy" select the
// LegacyConfigManager, "v2" selects the SeiConfigManager, and any other
// value is a hard error (no silent fallback).
func TestSelect(t *testing.T) {
	cases := []struct {
		name    string
		val     string
		want    ConfigManager
		wantErr bool
	}{
		{name: "unset", val: "", want: LegacyConfigManager{}},
		{name: "legacy", val: "legacy", want: LegacyConfigManager{}},
		{name: "v2", val: "v2", want: SeiConfigManager{}},
		{name: "garbage", val: "v3", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, err := Select(func(string) string { return tc.val })
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.IsType(t, tc.want, mgr)
		})
	}
}

// TestSeiConfigManagerNotImplemented asserts the v2 stub fails hard rather
// than silently behaving like legacy (PR1 ships the seam only).
func TestSeiConfigManagerNotImplemented(t *testing.T) {
	require.Error(t, (SeiConfigManager{}).Apply(nil, "", nil))
}

package configmanager

import (
	"fmt"
	"testing"
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
				if err == nil {
					t.Fatalf("Select(%q): want error, got nil", tc.val)
				}
				return
			}
			if err != nil {
				t.Fatalf("Select(%q): unexpected error: %v", tc.val, err)
			}
			if got, want := fmt.Sprintf("%T", mgr), fmt.Sprintf("%T", tc.want); got != want {
				t.Errorf("Select(%q) = %s, want %s", tc.val, got, want)
			}
		})
	}
}

// TestSeiConfigManagerNotImplemented asserts the v2 stub fails hard rather
// than silently behaving like legacy (PR1 ships the seam only).
func TestSeiConfigManagerNotImplemented(t *testing.T) {
	if err := (SeiConfigManager{}).Apply(nil, "", nil); err == nil {
		t.Fatal("SeiConfigManager.Apply: want not-implemented error, got nil")
	}
}

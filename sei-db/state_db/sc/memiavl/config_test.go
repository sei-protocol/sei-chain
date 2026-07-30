package memiavl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultConfigSnapshotKeepRecent(t *testing.T) {
	require.Equal(t, uint32(1), DefaultConfig().SnapshotKeepRecent)
}

func TestFillDefaultsSnapshotKeepRecent(t *testing.T) {
	tests := []struct {
		name string
		in   uint32
		want uint32
	}{
		{"zero uses default", 0, DefaultSnapshotKeepRecent},
		{"one stays", 1, 1},
		{"two stays", 2, 2},
		{"large stays", 100, 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{Config: Config{SnapshotKeepRecent: tc.in}}
			opts.FillDefaults()
			require.Equal(t, tc.want, opts.SnapshotKeepRecent)
		})
	}
}

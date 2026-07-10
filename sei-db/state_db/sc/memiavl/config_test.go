package memiavl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultConfigSnapshotKeepRecent(t *testing.T) {
	require.Equal(t, uint32(2), DefaultConfig().SnapshotKeepRecent)
}

func TestNormalizeSnapshotKeepRecent(t *testing.T) {
	tests := []struct {
		name string
		in   uint32
		want uint32
	}{
		{"zero clamps up to min", 0, 1},
		{"one stays", 1, 1},
		{"two stays", 2, 2},
		{"large stays", 100, 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, NormalizeSnapshotKeepRecent(tc.in))
		})
	}
}

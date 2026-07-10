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

func TestNormalizeSnapshotInterval(t *testing.T) {
	tests := []struct {
		name string
		in   uint32
		want uint32
	}{
		{"zero uses default cadence", 0, DefaultSnapshotInterval},
		{"one stays", 1, 1},
		{"default stays", DefaultSnapshotInterval, DefaultSnapshotInterval},
		{"custom stays", 5000, 5000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, NormalizeSnapshotInterval(tc.in))
		})
	}
}

// TestNormalizeSnapshotIntervalMatchesFillDefaults guards the invariant that the
// standalone normalizer agrees with Options.FillDefaults, which is what memIAVL
// actually applies at runtime.
func TestNormalizeSnapshotIntervalMatchesFillDefaults(t *testing.T) {
	for _, in := range []uint32{0, 1, DefaultSnapshotInterval, 5000} {
		opts := Options{Config: Config{SnapshotInterval: in}}
		opts.FillDefaults()
		require.Equal(t, opts.SnapshotInterval, NormalizeSnapshotInterval(in))
	}
}

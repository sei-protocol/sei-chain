package receipt

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Which driver enforces retention is a four-way decision, and getting it wrong in either direction
// is a production bug rather than a test nicety: two pruners race to different floors, and none
// lets the tag index grow without bound. Enumerated here rather than observed through the jittered
// ticker so every combination is covered without waiting on any of them.
func TestRunsLocalPruner(t *testing.T) {
	for _, tc := range []struct {
		name            string
		externalPruning bool
		keepRecent      int64
		pruneInterval   int64
		want            bool
	}{
		{
			name:          "standalone node prunes itself",
			keepRecent:    100_000,
			pruneInterval: 600,
			want:          true,
		},
		{
			name:            "under the collector it stands down",
			externalPruning: true,
			keepRecent:      100_000,
			pruneInterval:   600,
			want:            false,
		},
		{
			// KeepRecent 0 is the default and means keep everything, so there is nothing for a
			// local pruner to do. GetRetentionWindow maps the same 0 to InfiniteRetentionWindow.
			name:          "keep everything",
			keepRecent:    0,
			pruneInterval: 600,
			want:          false,
		},
		{
			name:          "no cadence configured",
			keepRecent:    100_000,
			pruneInterval: 0,
			want:          false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &littReceiptStore{
				externalPruning: tc.externalPruning,
				keepRecent:      tc.keepRecent,
				pruneInterval:   tc.pruneInterval,
			}
			require.Equal(t, tc.want, s.runsLocalPruner())
		})
	}
}

// The invariant behind the table: with retention configured at all, the local pruner is exactly the
// negation of ExternalPruning. Both on means two pruners racing to different floors; both off means
// nothing advances the floor. Asserted against the method the collector calls, so it covers the two
// staying wired to each other and not merely today's values.
func TestRunsLocalPrunerIsTheNegationOfExternalPruning(t *testing.T) {
	for _, external := range []bool{false, true} {
		s := &littReceiptStore{externalPruning: external, keepRecent: 100_000, pruneInterval: 600}
		require.Equal(t, !s.ExternalPruning(), s.runsLocalPruner(),
			"exactly one of the collector and the local pruner may enforce retention")
	}
}

package flatkv

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/stretchr/testify/require"
)

// TestCatchupRejectsWALGap verifies that catchup refuses to advance when the
// WAL no longer covers committedVersion+1. This guards against the auditor's
// scenario where a tooling clone copies a snapshot and then a stale WAL whose
// front has been truncated past the snapshot version: the previous code
// silently jumped to the WAL's first available entry and corrupted the
// running LtHash/committed metadata.
func TestCatchupRejectsWALGap(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	for i := byte(1); i <= 5; i++ {
		commitStorageEntry(t, s, ktype.Address{i}, ktype.Slot{i}, []byte{i})
	}
	require.Equal(t, int64(5), s.committedVersion)

	off, err := s.walOffsetForVersion(4)
	require.NoError(t, err)
	require.NoError(t, s.changelog.TruncateBefore(off))

	s.committedVersion = 2

	err = s.catchup(0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "WAL gap")
}

// TestCatchupNoOpWhenWALBehindCommittedVersion verifies catchup is a clean
// no-op when the WAL only contains entries that are already covered by
// committedVersion (the normal post-truncation steady state).
func TestCatchupNoOpWhenWALBehindCommittedVersion(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	for i := byte(1); i <= 3; i++ {
		commitStorageEntry(t, s, ktype.Address{i}, ktype.Slot{i}, []byte{i})
	}
	require.Equal(t, int64(3), s.committedVersion)

	require.NoError(t, s.catchup(0))
	require.Equal(t, int64(3), s.committedVersion)
}

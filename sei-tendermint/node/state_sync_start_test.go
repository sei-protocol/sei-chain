package node

import (
	"os"
	"strconv"
	"testing"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// stateSyncStartTest is a fullnode home and its giga config, with the switch on.
type stateSyncStartTest struct {
	conf   *config.Config
	giga   gigaconfig.Config
	layout AutobahnLayout
}

func newStateSyncStartTest(t *testing.T) *stateSyncStartTest {
	t.Helper()
	conf := layoutConfig(t, t.TempDir())
	giga := gigaconfig.DefaultConfig
	giga.StateSync.Enabled = true
	layout, err := ResolveAutobahnLayout(conf, giga.Storage, genesisAt(1))
	require.NoError(t, err)
	return &stateSyncStartTest{conf: conf, giga: giga, layout: layout}
}

// mark writes the restore marker for height, and the restore anchor of height when anchor is set.
func (s *stateSyncStartTest) mark(t *testing.T, height int64, anchor bool) {
	t.Helper()
	require.NoError(t, os.WriteFile(s.layout.RestoreMarkerFile, []byte(strconv.FormatInt(height, 10)+"\n"), 0o600))
	if anchor {
		genesis, err := ResolveEpochGenesis(s.conf, genesisAt(1))
		require.NoError(t, err)
		require.NoError(t, StoreRestoreAnchor(s.layout, genesis, height, [32]byte{1}))
	}
}

func TestReadStateSyncStartWithoutAMarker(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		s := newStateSyncStartTest(t)
		s.giga.StateSync.Enabled = enabled
		start, err := readStateSyncStart(s.conf, s.giga, genesisAt(1))
		require.NoError(t, err)
		require.Equal(t, enabled, start.Enabled)
		require.False(t, start.Restored.IsPresent())
	}
}

func TestReadStateSyncStartRefusesAMarkedHomeItCannotSeed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   func(t *testing.T, s *stateSyncStartTest)
		wantMsg string
	}{
		{
			name: "the switch is off",
			setup: func(t *testing.T, s *stateSyncStartTest) {
				s.giga.StateSync.Enabled = false
				s.mark(t, 10, true)
			},
			wantMsg: "set [giga.state_sync] enabled = true",
		},
		{
			name: "the node runs as a validator",
			setup: func(t *testing.T, s *stateSyncStartTest) {
				s.conf.Mode = config.ModeValidator
				s.mark(t, 10, true)
			},
			wantMsg: "must start once as a fullnode",
		},
		{
			name:    "FlatKV is not installed",
			setup:   func(t *testing.T, s *stateSyncStartTest) { s.mark(t, 10, true) },
			wantMsg: "the restore of 10 on this home is incomplete, run restore again: FlatKV is at 0",
		},
		{
			name: "the marker does not hold a height",
			setup: func(t *testing.T, s *stateSyncStartTest) {
				require.NoError(t, os.WriteFile(s.layout.RestoreMarkerFile, []byte("ten\n"), 0o600))
			},
			wantMsg: "want one positive decimal height",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStateSyncStartTest(t)
			tc.setup(t, s)
			_, err := readStateSyncStart(s.conf, s.giga, genesisAt(1))
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantMsg)
			require.FileExists(t, s.layout.RestoreMarkerFile)
		})
	}
}

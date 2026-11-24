package node

import (
	"os"
	"strconv"
	"testing"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
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
	layout, err := ResolveAutobahnLayout(conf)
	require.NoError(t, err)
	return &stateSyncStartTest{conf: conf, giga: giga, layout: layout}
}

// readStart runs readStateSyncStart on the home, as makeEVMApplication does.
func (s *stateSyncStartTest) readStart(t *testing.T) (p2p.StateSyncStart, error) {
	t.Helper()
	fc, _, err := loadAutobahnCommittee(s.conf.AutobahnConfigFile)
	require.NoError(t, err)
	return readStateSyncStart(s.conf, fc, s.giga, genesisAt(1))
}

// mark writes the state sync restore marker for height and the restore anchor of height.
func (s *stateSyncStartTest) mark(t *testing.T, height int64) {
	t.Helper()
	require.NoError(t, os.WriteFile(s.layout.RestoreMarkerFile, []byte(strconv.FormatInt(height, 10)+"\n"), 0o600))
	require.NoError(t, WriteRestoredAppHash(s.layout, s.conf.Mode, s.giga.Storage, genesisAt(1), height, [32]byte{1}))
}

func TestReadStateSyncStartWithoutAMarker(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		s := newStateSyncStartTest(t)
		s.giga.StateSync.Enabled = enabled
		start, err := s.readStart(t)
		require.NoError(t, err)
		require.Equal(t, enabled, start.Enabled)
		require.False(t, start.PendingRestore.IsPresent())
	}
}

func TestReadStateSyncStartRefusesAPendingRestoreItCannotFinish(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   func(t *testing.T, s *stateSyncStartTest)
		wantErr error
	}{
		{
			name: "the switch is off",
			setup: func(t *testing.T, s *stateSyncStartTest) {
				s.giga.StateSync.Enabled = false
				s.mark(t, 10)
			},
			wantErr: errRestoreNeedsStateSync,
		},
		{
			name: "the node runs as a validator",
			setup: func(t *testing.T, s *stateSyncStartTest) {
				s.mark(t, 10)
				s.conf.Mode = config.ModeValidator
			},
			wantErr: errRestoreNeedsFullnode,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStateSyncStartTest(t)
			tc.setup(t, s)
			_, err := s.readStart(t)
			require.ErrorIs(t, err, tc.wantErr)
			require.FileExists(t, s.layout.RestoreMarkerFile)
		})
	}
}

func TestReadStateSyncStartRefusesARestoreWithoutFlatKV(t *testing.T) {
	// The switch is on, the node is a fullnode and the app-hash vault is at 10, so the restore
	// lacks only FlatKV.
	s := newStateSyncStartTest(t)
	s.mark(t, 10)
	_, err := s.readStart(t)
	require.Error(t, err)
	require.FileExists(t, s.layout.RestoreMarkerFile)
}

func TestReadStateSyncStartRefusesAMarkerThatHoldsNoHeight(t *testing.T) {
	s := newStateSyncStartTest(t)
	require.NoError(t, os.WriteFile(s.layout.RestoreMarkerFile, []byte("ten\n"), 0o600))
	_, err := s.readStart(t)
	require.Error(t, err)
	require.FileExists(t, s.layout.RestoreMarkerFile)
}

func TestMakeEVMApplicationRefusesAPendingRestoreItCannotFinish(t *testing.T) {
	s := newStateSyncStartTest(t)
	s.giga.StateSync.Enabled = false
	s.mark(t, 10)
	_, _, _, err := makeEVMApplication(storageContext(t), s.conf, s.giga, genesisAt(1))
	require.ErrorIs(t, err, errRestoreNeedsStateSync)
}

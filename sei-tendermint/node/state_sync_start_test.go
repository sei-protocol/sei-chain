package node

import (
	"math"
	"os"
	"strconv"
	"testing"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evmonlyapp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
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
func (s *stateSyncStartTest) readStart(t *testing.T) (stateSyncStart, error) {
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

// restore leaves the home as an import of road 1 leaves it, except for the state sync proof file:
// FlatKV at 1 with the epoch registry row of fixture, the restore marker and the restore anchor.
func (s *stateSyncStartTest) restore(t *testing.T, fixture StateSyncProofFixture) {
	t.Helper()
	commitRow(t, s.layout.FlatKVDir, fixture.Module, fixture.Key, fixture.Value)
	s.mark(t, 1)
}

// writeProof installs proof as the state sync proof file of the home.
func (s *stateSyncStartTest) writeProof(t *testing.T, proof []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(s.layout.RestoreProofFile, proof, 0o600))
}

// withoutNextEpoch returns fixture with the epoch registry row of its registry without the newest
// epoch, which is the epoch after the epoch of the road of fixture.
func withoutNextEpoch(t *testing.T, fixture StateSyncProofFixture) StateSyncProofFixture {
	t.Helper()
	registry, err := epoch.Decode(fixture.Value)
	require.NoError(t, err)
	short, err := registry.DiscardNewest()
	require.NoError(t, err)
	fixture.Module, fixture.Key, fixture.Value = evmonlyapp.EpochRegistryRowForTesting(short)
	return fixture
}

func TestReadStateSyncStartWithoutAMarker(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		s := newStateSyncStartTest(t)
		s.giga.StateSync.Enabled = enabled
		start, err := s.readStart(t)
		require.NoError(t, err)
		require.Equal(t, enabled, start.enabled)
		require.False(t, start.pendingRestore.IsPresent())
		require.False(t, start.seed.IsPresent())
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

func TestReadStateSyncStartReadsTheSeedOfAPendingRestore(t *testing.T) {
	fixture := utils.OrPanic1(GenStateSyncProofFixture(utils.TestRng(), 1))
	s := newStateSyncStartTest(t)
	s.restore(t, fixture)
	s.writeProof(t, fixture.Proof)

	start, err := s.readStart(t)
	require.NoError(t, err)
	require.Equal(t, utils.Some[atypes.RoadHeight](1), start.pendingRestore)
	seed := start.seed.OrPanic("the seed of the pending restore")
	require.Equal(t, atypes.RoadHeight(1), seed.Block.QC().Height())
	require.Equal(t, atypes.AppHash(fixture.AppHash[:]), seed.AppQC.Proposal().AppHash())
}

func TestReadStateSyncStartWithoutAProofFileReadsNoSeed(t *testing.T) {
	// A start that removed the state sync proof file and then stopped leaves this home.
	s := newStateSyncStartTest(t)
	s.restore(t, utils.OrPanic1(GenStateSyncProofFixture(utils.TestRng(), 1)))

	start, err := s.readStart(t)
	require.NoError(t, err)
	require.Equal(t, utils.Some[atypes.RoadHeight](1), start.pendingRestore)
	require.False(t, start.seed.IsPresent())
}

func TestReadStateSyncStartRefusesAProofOfAnotherCommittee(t *testing.T) {
	rng := utils.TestRng()
	fixture := utils.OrPanic1(GenStateSyncProofFixture(rng, 1))
	other := utils.OrPanic1(GenStateSyncProofFixture(rng, 1))
	s := newStateSyncStartTest(t)
	s.restore(t, fixture)
	s.writeProof(t, other.Proof)

	_, err := s.readStart(t)
	require.Error(t, err)
	require.FileExists(t, s.layout.RestoreMarkerFile)
}

func TestReadStateSyncStartRefusesACheckpointWithoutTheNextEpoch(t *testing.T) {
	fixture := utils.OrPanic1(GenStateSyncProofFixture(utils.TestRng(), 1))
	s := newStateSyncStartTest(t)
	s.restore(t, withoutNextEpoch(t, fixture))
	s.writeProof(t, fixture.Proof)

	_, err := s.readStart(t)
	require.ErrorIs(t, err, errRestoreNeedsNextEpoch)
	require.FileExists(t, s.layout.RestoreMarkerFile)
}

func TestReadRestoreMarker(t *testing.T) {
	psd := t.TempDir()
	got, err := readRestoreMarker(psd)
	require.NoError(t, err)
	require.False(t, got.IsPresent())

	require.NoError(t, os.WriteFile(p2p.RestoreMarkerFile(psd), []byte("7\n"), 0o600))
	got, err = readRestoreMarker(psd)
	require.NoError(t, err)
	require.Equal(t, utils.Some[atypes.RoadHeight](7), got)

	require.NoError(t, os.WriteFile(p2p.RestoreMarkerFile(psd), []byte("9223372036854775807\n"), 0o600))
	got, err = readRestoreMarker(psd)
	require.NoError(t, err)
	require.Equal(t, utils.Some[atypes.RoadHeight](math.MaxInt64), got)

	for _, raw := range []string{"", "0\n", "x\n", "7 8\n", "-1\n", "9223372036854775808\n"} {
		require.NoError(t, os.WriteFile(p2p.RestoreMarkerFile(psd), []byte(raw), 0o600))
		_, err := readRestoreMarker(psd)
		require.Error(t, err, "marker %q", raw)
	}
}

func TestMakeEVMApplicationRefusesAPendingRestoreItCannotFinish(t *testing.T) {
	s := newStateSyncStartTest(t)
	s.giga.StateSync.Enabled = false
	s.mark(t, 10)
	_, _, _, err := makeEVMApplication(storageContext(t), s.conf, s.giga, genesisAt(1))
	require.ErrorIs(t, err, errRestoreNeedsStateSync)
}

package p2p

import (
	"math"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const seedRoad atypes.RoadHeight = 5

var seedAppHash = [32]byte{1, 2, 3}

// seedApp is a test app whose epoch registry is registry.
type seedApp struct {
	*testApp
	registry *atypes.EpochRegistry
}

func (a seedApp) EpochRegistry() (*atypes.EpochRegistry, error) { return a.registry, nil }

// changingRegistryApp is a test app whose epoch registry is first at its first read and later at
// each read after that.
type changingRegistryApp struct {
	*testApp
	first, later *atypes.EpochRegistry
	reads        *int
}

func (a changingRegistryApp) EpochRegistry() (*atypes.EpochRegistry, error) {
	*a.reads++
	if *a.reads == 1 {
		return a.first, nil
	}
	return a.later, nil
}

// seedTest is a fullnode home of a one-validator chain whose epoch 0 starts at seedRoad, with an
// empty BlockDB.
type seedTest struct {
	rng      utils.Rng
	key      atypes.SecretKey
	registry *atypes.EpochRegistry
	cfg      *GigaRouterCommonConfig
	app      *abevm.Proxy
	hashes   *manualAppHashBuilder
	store    *blockstore.Store
}

func newSeedTest(t *testing.T, rng utils.Rng) *seedTest {
	t.Helper()
	key := atypes.GenSecretKey(rng)
	registry, err := atypes.GenesisRegistry(map[atypes.PublicKey]uint64{key.Public(): 1}, seedRoad, 10)
	require.NoError(t, err)
	genDoc := &tmtypes.GenesisDoc{
		ChainID:         "seed-test",
		InitialHeight:   1,
		GenesisTime:     time.Now(),
		AppState:        testAppStateJSON(rng),
		ConsensusParams: tmtypes.DefaultConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())
	hashes := newManualAppHashBuilder()
	app := abevm.NewProxy(seedApp{testApp: newTestApp(genDoc.AppState), registry: registry})
	return &seedTest{
		rng:      rng,
		key:      key,
		registry: registry,
		cfg: &GigaRouterCommonConfig{
			DialInterval:       time.Second,
			ValidatorAddrs:     map[atypes.PublicKey]GigaNodeAddr{key.Public(): {}},
			PersistentStateDir: t.TempDir(),
			GenDoc:             genDoc,
			App:                app,
			AppHashBuilder:     hashes,
		},
		app:    app,
		hashes: hashes,
		store:  blockstore.NewTestStore(t),
	}
}

// setRegistries gives the home an app whose epoch registry is first at its first read and later at
// each read after that.
func (s *seedTest) setRegistries(first, later *atypes.EpochRegistry) {
	s.app = abevm.NewProxy(changingRegistryApp{testApp: newTestApp(s.cfg.GenDoc.AppState), first: first, later: later, reads: new(int)})
	s.cfg.App = s.app
}

// registryWithoutNextEpoch returns the epoch registry of the chain without the epoch after the
// epoch of seedRoad.
func (s *seedTest) registryWithoutNextEpoch(t *testing.T) *atypes.EpochRegistry {
	t.Helper()
	registry, err := s.registry.DiscardNewest()
	require.NoError(t, err)
	return registry
}

// proof returns a state sync proof file of the first road of ep that signs appHash, signed by key.
func (s *seedTest) proof(t *testing.T, ep *atypes.Epoch, key atypes.SecretKey, appHash [32]byte) []byte {
	t.Helper()
	data, err := statesyncproof.EncodeWithKeysForTesting(s.rng, ep, []atypes.SecretKey{key}, appHash[:])
	require.NoError(t, err)
	return data
}

// epochOf returns epoch 0 over roads with key as its one validator.
func epochOf(t *testing.T, key atypes.SecretKey, roads atypes.RoadRange) *atypes.Epoch {
	t.Helper()
	committee, err := atypes.NewCommittee(map[atypes.PublicKey]uint64{key.Public(): 1})
	require.NoError(t, err)
	return atypes.NewEpoch(0, roads, committee)
}

// restoredProof returns the state sync proof file of seedRoad that the committee of the chain signs.
func (s *seedTest) restoredProof(t *testing.T) []byte {
	t.Helper()
	return s.proof(t, s.registry.Epochs()[0], s.key, seedAppHash)
}

// restore leaves the home as a restore of seedRoad leaves it: the state sync restore marker, the
// state sync proof file proof, and the restore anchor of seedRoad with anchor in the app-hash vault.
func (s *seedTest) restore(t *testing.T, proof []byte, anchor [32]byte) {
	t.Helper()
	psd := s.cfg.PersistentStateDir
	require.NoError(t, os.WriteFile(RestoreMarkerFile(psd), []byte(strconv.FormatUint(uint64(seedRoad), 10)+"\n"), 0o600))
	require.NoError(t, os.WriteFile(RestoreProofFile(psd), proof, 0o600))
	s.hashes.add(apphash.NewRestoreAnchorAppHashData(0, uint64(seedRoad), anchor))
}

// start builds the data state of the home with a pending restore of seedRoad.
func (s *seedTest) start(t *testing.T) (*data.State, error) {
	t.Helper()
	return BuildFullnodeDataState(t.Context(), s.cfg, s.store, s.app, s.cfg.GenDoc.GenesisTime,
		StateSyncStart{Enabled: true, PendingRestore: utils.Some(seedRoad)})
}

// requireSeeded asserts that the BlockDB holds exactly the records of seedRoad and that the
// restore files are gone.
func (s *seedTest) requireSeeded(t *testing.T) {
	t.Helper()
	status, ok := s.store.Status().Get()
	require.True(t, ok)
	require.Equal(t, blockstore.SuffixRange{
		First:           seedRoad,
		NextBlock:       seedRoad + 1,
		NextAppProposal: seedRoad + 1,
		NextAppQC:       seedRoad + 1,
	}, status)
	require.NoFileExists(t, RestoreMarkerFile(s.cfg.PersistentStateDir))
	require.NoFileExists(t, RestoreProofFile(s.cfg.PersistentStateDir))
}

// requireUnseeded asserts that the BlockDB is empty and that the marker remains, so the next start
// seeds again.
func (s *seedTest) requireUnseeded(t *testing.T) {
	t.Helper()
	require.False(t, s.store.Status().IsPresent())
	require.FileExists(t, RestoreMarkerFile(s.cfg.PersistentStateDir))
}

func TestSeedWritesTheRestoredRoad(t *testing.T) {
	s := newSeedTest(t, utils.TestRng())
	s.restore(t, s.restoredProof(t), seedAppHash)
	dataState, err := s.start(t)
	require.NoError(t, err)
	s.requireSeeded(t)
	require.Equal(t, seedRoad+1, dataState.NextAppProposal())
}

func TestSeedCompletesTheRecordsThatAnEarlierSeedFlushed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		flushed int
		// keepProof is false when the earlier seed also removed the state sync proof file.
		keepProof bool
	}{
		{name: "the road block", flushed: 1, keepProof: true},
		{name: "the road block and the AppProposal", flushed: 2, keepProof: true},
		{name: "all three records", flushed: 3, keepProof: true},
		{name: "all three records and no proof file", flushed: 3, keepProof: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSeedTest(t, utils.TestRng())
			data := s.restoredProof(t)
			s.restore(t, data, seedAppHash)
			proof, err := statesyncproof.Decode(data, seedRoad)
			require.NoError(t, err)
			writes := []func() error{
				func() error { return s.store.WriteBlock(proof.RoadBlock()) },
				func() error { return s.store.WriteAppProposal(proof.AppQC().Proposal()) },
				func() error { return s.store.WriteAppQC(proof.AppQC()) },
			}
			for _, write := range writes[:tc.flushed] {
				require.NoError(t, write())
			}
			require.NoError(t, s.store.Flush())
			if !tc.keepProof {
				require.NoError(t, os.Remove(RestoreProofFile(s.cfg.PersistentStateDir)))
			}
			_, err = s.start(t)
			require.NoError(t, err)
			s.requireSeeded(t)
		})
	}
}

func TestSeedRefusesAStateItCannotTrust(t *testing.T) {
	// Each home differs from the one that TestSeedWritesTheRestoredRoad seeds only in what its
	// name says.
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, s *seedTest)
	}{
		{
			name: "a proof signed by another committee",
			setup: func(t *testing.T, s *seedTest) {
				other := atypes.GenSecretKey(s.rng)
				ep := s.registry.Epochs()[0]
				s.restore(t, s.proof(t, epochOf(t, other, ep.RoadRange()), other, seedAppHash), seedAppHash)
			},
		},
		{
			name: "a restore anchor that the AppQC does not sign",
			setup: func(t *testing.T, s *seedTest) {
				s.restore(t, s.restoredProof(t), [32]byte{9})
			},
		},
		{
			name: "no state sync proof file",
			setup: func(t *testing.T, s *seedTest) {
				s.restore(t, s.restoredProof(t), seedAppHash)
				require.NoError(t, os.Remove(RestoreProofFile(s.cfg.PersistentStateDir)))
			},
		},
		{
			name: "a registry with no epoch after the epoch of the restored road",
			setup: func(t *testing.T, s *seedTest) {
				short := s.registryWithoutNextEpoch(t)
				s.setRegistries(short, short)
				s.restore(t, s.restoredProof(t), seedAppHash)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSeedTest(t, utils.TestRng())
			tc.setup(t, s)
			_, err := s.start(t)
			require.Error(t, err)
			s.requireUnseeded(t)
		})
	}
}

func TestSeedKeepsTheRestoreFilesUntilTheDataStateAcceptsTheBlockDB(t *testing.T) {
	s := newSeedTest(t, utils.TestRng())
	s.setRegistries(s.registry, s.registryWithoutNextEpoch(t))
	s.restore(t, s.restoredProof(t), seedAppHash)
	_, err := s.start(t)
	require.ErrorIs(t, err, atypes.ErrNotFound)
	require.FileExists(t, RestoreMarkerFile(s.cfg.PersistentStateDir))
	require.FileExists(t, RestoreProofFile(s.cfg.PersistentStateDir))

	s.setRegistries(s.registry, s.registry)
	_, err = s.start(t)
	require.NoError(t, err)
	s.requireSeeded(t)
}

func TestSeedRefusesABlockDBWithAnotherRoad(t *testing.T) {
	s := newSeedTest(t, utils.TestRng())
	s.restore(t, s.restoredProof(t), seedAppHash)
	earlierEpoch := epochOf(t, s.key, atypes.RoadRange{First: seedRoad - 1, Next: seedRoad + 9})
	earlier, err := statesyncproof.Decode(s.proof(t, earlierEpoch, s.key, seedAppHash), seedRoad-1)
	require.NoError(t, err)
	require.NoError(t, s.store.WriteBlock(earlier.RoadBlock()))
	before := s.store.Status()
	_, err = s.start(t)
	require.Error(t, err)
	// The seed wrote nothing, and the next start meets the same home.
	require.Equal(t, before, s.store.Status())
	require.FileExists(t, RestoreMarkerFile(s.cfg.PersistentStateDir))
}

func TestBuildFullnodeDataStateWithoutARestoreWritesNothing(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run("enabled="+strconv.FormatBool(enabled), func(t *testing.T) {
			s := newSeedTest(t, utils.TestRng())
			_, err := BuildFullnodeDataState(t.Context(), s.cfg, s.store, s.app, s.cfg.GenDoc.GenesisTime, StateSyncStart{Enabled: enabled})
			require.NoError(t, err)
			require.False(t, s.store.Status().IsPresent())
		})
	}
}

func TestReadRestoreMarker(t *testing.T) {
	psd := t.TempDir()
	got, err := ReadRestoreMarker(psd)
	require.NoError(t, err)
	require.False(t, got.IsPresent())

	require.NoError(t, os.WriteFile(RestoreMarkerFile(psd), []byte("7\n"), 0o600))
	got, err = ReadRestoreMarker(psd)
	require.NoError(t, err)
	require.Equal(t, utils.Some[atypes.RoadHeight](7), got)

	require.NoError(t, os.WriteFile(RestoreMarkerFile(psd), []byte("9223372036854775807\n"), 0o600))
	got, err = ReadRestoreMarker(psd)
	require.NoError(t, err)
	require.Equal(t, utils.Some[atypes.RoadHeight](math.MaxInt64), got)

	for _, raw := range []string{"", "0\n", "x\n", "7 8\n", "-1\n", "9223372036854775808\n"} {
		require.NoError(t, os.WriteFile(RestoreMarkerFile(psd), []byte(raw), 0o600))
		_, err := ReadRestoreMarker(psd)
		require.Error(t, err, "marker %q", raw)
	}
}

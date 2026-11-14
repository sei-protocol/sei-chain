package p2p

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const seedRoad atypes.RoadIndex = 5

var seedAppHash = [32]byte{1, 2, 3}

// seedTest is a fullnode home of a one-validator chain, with an empty BlockDB.
type seedTest struct {
	key    atypes.SecretKey
	cfg    *GigaRouterCommonConfig
	hashes *manualAppHashBuilder
	store  *blockstore.Store
}

func newSeedTest(t *testing.T) *seedTest {
	t.Helper()
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	genDoc := &tmtypes.GenesisDoc{
		ChainID:         "seed-test",
		InitialHeight:   1,
		GenesisTime:     time.Now(),
		AppState:        testAppStateJSON(rng),
		ConsensusParams: tmtypes.DefaultConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())
	hashes := newManualAppHashBuilder()
	return &seedTest{
		key: key,
		cfg: &GigaRouterCommonConfig{
			DialInterval:       time.Second,
			ValidatorAddrs:     map[atypes.PublicKey]GigaNodeAddr{key.Public(): {}},
			PersistentStateDir: t.TempDir(),
			GenDoc:             genDoc,
			App:                proxy.NewEVM(newTestApp()),
			AppHashBuilder:     hashes,
		},
		hashes: hashes,
		store:  blockstore.NewTestStore(t),
	}
}

// proof returns a state sync proof file of road that signs appHash, signed by key as the one
// genesis validator.
func (s *seedTest) proof(t *testing.T, key atypes.SecretKey, road atypes.RoadIndex, appHash [32]byte) []byte {
	t.Helper()
	registry, err := OpenEpochRegistry(s.cfg.GenDoc, []atypes.PublicKey{key.Public()}, t.TempDir())
	require.NoError(t, err)
	ep, err := registry.EpochAt(road)
	require.NoError(t, err)
	data, err := statesyncproof.EncodeForTesting(ep, []atypes.SecretKey{key}, road, appHash[:])
	require.NoError(t, err)
	return data
}

// restore leaves the home as a restore of seedRoad leaves it: the restore marker, the state sync
// proof file proof, and the restore anchor of seedRoad with anchor in the app-hash vault.
func (s *seedTest) restore(t *testing.T, proof []byte, anchor [32]byte) {
	t.Helper()
	psd := s.cfg.PersistentStateDir
	require.NoError(t, os.WriteFile(RestoreMarkerFile(psd), []byte(strconv.FormatUint(uint64(seedRoad), 10)+"\n"), 0o600))
	require.NoError(t, os.WriteFile(RestoreStateSyncProofFile(psd), proof, 0o600))
	s.hashes.add(apphash.NewRestoreAnchorAppHashData(0, uint64(seedRoad), anchor))
}

func (s *seedTest) start(t *testing.T) error {
	t.Helper()
	_, err := BuildFullnodeDataState(t.Context(), s.cfg, s.store, StateSyncStart{Enabled: true, Restored: utils.Some(seedRoad)})
	return err
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
	require.NoFileExists(t, RestoreStateSyncProofFile(s.cfg.PersistentStateDir))
}

// requireUnseeded asserts that the BlockDB is empty and that the restore files remain, so the
// next start seeds again.
func (s *seedTest) requireUnseeded(t *testing.T) {
	t.Helper()
	require.False(t, s.store.Status().IsPresent())
	require.FileExists(t, RestoreMarkerFile(s.cfg.PersistentStateDir))
}

func TestSeedWritesTheRestoredRoad(t *testing.T) {
	s := newSeedTest(t)
	s.restore(t, s.proof(t, s.key, seedRoad, seedAppHash), seedAppHash)
	dataState, err := BuildFullnodeDataState(t.Context(), s.cfg, s.store, StateSyncStart{Enabled: true, Restored: utils.Some(seedRoad)})
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
			s := newSeedTest(t)
			data := s.proof(t, s.key, seedRoad, seedAppHash)
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
				require.NoError(t, os.Remove(RestoreStateSyncProofFile(s.cfg.PersistentStateDir)))
			}
			require.NoError(t, s.start(t))
			s.requireSeeded(t)
		})
	}
}

func TestSeedRefusesAStateItCannotTrust(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   func(t *testing.T, s *seedTest)
		wantMsg string
	}{
		{
			name: "a proof signed by another committee",
			setup: func(t *testing.T, s *seedTest) {
				other := atypes.SecretKeyFromED25519(ed25519.TestSecretKey([]byte("another validator")))
				s.restore(t, s.proof(t, other, seedRoad, seedAppHash), seedAppHash)
			},
			wantMsg: "verify the state sync proof",
		},
		{
			name: "a restore anchor that the AppQC does not sign",
			setup: func(t *testing.T, s *seedTest) {
				s.restore(t, s.proof(t, s.key, seedRoad, seedAppHash), [32]byte{9})
			},
			wantMsg: "but the AppQC of the state sync proof file signs",
		},
		{
			name: "no state sync proof file",
			setup: func(t *testing.T, s *seedTest) {
				s.restore(t, s.proof(t, s.key, seedRoad, seedAppHash), seedAppHash)
				require.NoError(t, os.Remove(RestoreStateSyncProofFile(s.cfg.PersistentStateDir)))
			},
			wantMsg: "run restore again",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSeedTest(t)
			tc.setup(t, s)
			err := s.start(t)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantMsg)
			s.requireUnseeded(t)
		})
	}
}

func TestSeedRefusesABlockDBWithAnotherRoad(t *testing.T) {
	s := newSeedTest(t)
	s.restore(t, s.proof(t, s.key, seedRoad, seedAppHash), seedAppHash)
	earlier, err := statesyncproof.Decode(s.proof(t, s.key, seedRoad-1, seedAppHash), seedRoad-1)
	require.NoError(t, err)
	require.NoError(t, s.store.WriteBlock(earlier.RoadBlock()))
	err = s.start(t)
	require.Error(t, err)
	require.Contains(t, err.Error(), "want at most the records of the restored road")
	require.FileExists(t, RestoreMarkerFile(s.cfg.PersistentStateDir))
}

func TestBuildFullnodeDataStateWithoutARestoreWritesNothing(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run("enabled="+strconv.FormatBool(enabled), func(t *testing.T) {
			s := newSeedTest(t)
			_, err := BuildFullnodeDataState(t.Context(), s.cfg, s.store, StateSyncStart{Enabled: enabled})
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
	require.Equal(t, utils.Some[atypes.RoadIndex](7), got)

	for _, raw := range []string{"", "0\n", "x\n", "7 8\n", "-1\n"} {
		require.NoError(t, os.WriteFile(RestoreMarkerFile(psd), []byte(raw), 0o600))
		_, err := ReadRestoreMarker(psd)
		require.Error(t, err, "marker %q", raw)
	}
}

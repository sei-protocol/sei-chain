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
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// restoredHome is a persistent state dir that a state sync import of road prepared, with the app
// hash of road in appHashBuilder.
type restoredHome struct {
	psd            string
	road           atypes.RoadHeight
	registry       *epoch.TestRegistry
	appHashBuilder *manualAppHashBuilder
}

func newRestoredHome(t *testing.T, rng utils.Rng) restoredHome {
	t.Helper()
	registry, keys := epoch.GenRegistry(rng, 4)
	ep := registry.Epochs()[0]
	qc, blocks := data.TestCommitQC(rng, ep, keys, utils.None[*atypes.CommitQC]())
	appHash := utils.GenBytes(rng, 32)
	appQC := data.TestAppQC(keys, atypes.NewAppProposal(qc.QC().View(), appHash))
	proof, err := statesyncproof.Encode(atypes.NewRoadBlock(qc, blocks), appQC)
	require.NoError(t, err)

	road := qc.Height()
	psd := t.TempDir()
	require.NoError(t, os.WriteFile(RestoreMarkerFile(psd), []byte(strconv.FormatUint(uint64(road), 10)+"\n"), 0o600))
	require.NoError(t, os.WriteFile(RestoreProofFile(psd), proof, 0o600))
	appHashBuilder := newManualAppHashBuilder()
	appHashBuilder.add(apphash.NewRestoreAnchorAppHashData(0, uint64(road), [32]byte(appHash)))
	return restoredHome{psd: psd, road: road, registry: registry, appHashBuilder: appHashBuilder}
}

func requireRestoreFiles(t *testing.T, psd string, want bool) {
	t.Helper()
	for _, path := range []string{RestoreMarkerFile(psd), RestoreProofFile(psd)} {
		_, err := os.Stat(path)
		require.Equal(t, want, err == nil, path)
	}
}

func TestSeedRestoredBlockStoreWritesTheRestoredRoad(t *testing.T) {
	home := newRestoredHome(t, utils.TestRng())
	db := blockstore.NewTestStore(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	require.NoError(t, SeedRestoredBlockStore(t.Context(), home.psd, home.registry.Snapshot(), home.appHashBuilder, db))

	road := home.road
	require.Equal(t, utils.Some(blockstore.SuffixRange{First: road, NextBlock: road + 1, NextAppProposal: road + 1, NextAppQC: road + 1}), db.Status())
	requireRestoreFiles(t, home.psd, false)
	_, err := data.NewState(&data.Config{App: proxy.TestEpochApp(home.registry), GenesisTimestamp: time.Time{}}, db)
	require.NoError(t, err)
}

func TestSeedRestoredBlockStoreWithoutAMarkerWritesNothing(t *testing.T) {
	home := newRestoredHome(t, utils.TestRng())
	require.NoError(t, os.Remove(RestoreMarkerFile(home.psd)))
	db := blockstore.NewTestStore(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	require.NoError(t, SeedRestoredBlockStore(t.Context(), home.psd, home.registry.Snapshot(), home.appHashBuilder, db))

	require.False(t, db.Status().IsPresent())
}

func TestSeedRestoredBlockStoreRefusesAProofOfAnotherCommittee(t *testing.T) {
	rng := utils.TestRng()
	home := newRestoredHome(t, rng)
	other, _ := epoch.GenRegistry(rng, 4)
	db := blockstore.NewTestStore(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	require.Error(t, SeedRestoredBlockStore(t.Context(), home.psd, other.Snapshot(), home.appHashBuilder, db))

	require.False(t, db.Status().IsPresent())
	requireRestoreFiles(t, home.psd, true)
}

func TestSeedRestoredBlockStoreRefusesAnotherRestoreAnchor(t *testing.T) {
	home := newRestoredHome(t, utils.TestRng())
	home.appHashBuilder.add(apphash.NewRestoreAnchorAppHashData(0, uint64(home.road), [32]byte{1}))
	db := blockstore.NewTestStore(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	require.Error(t, SeedRestoredBlockStore(t.Context(), home.psd, home.registry.Snapshot(), home.appHashBuilder, db))

	require.False(t, db.Status().IsPresent())
	requireRestoreFiles(t, home.psd, true)
}

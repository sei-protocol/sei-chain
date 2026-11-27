package statesync

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// fullnodeHome is a fullnode home of the test chain with giga state sync enabled, laid out by its
// config files as node startup reads them.
type fullnodeHome struct {
	conf   *tmcfg.Config
	giga   gigaconfig.Config
	genDoc *tmtypes.GenesisDoc
	target target
}

func newFullnodeHome(t *testing.T) fullnodeHome {
	t.Helper()
	conf := tmcfg.TestConfig().SetRoot(t.TempDir())
	conf.Mode = tmcfg.ModeFull

	genDoc := &tmtypes.GenesisDoc{
		ChainID:       testChainID,
		InitialHeight: 1,
		GenesisTime:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(conf.GenesisFile()), 0o750))
	require.NoError(t, genDoc.SaveAs(conf.GenesisFile()))

	address, err := tcp.ParseHostPort("localhost:26660")
	require.NoError(t, err)
	validator := tmcfg.AutobahnValidator{
		ValidatorKey: atypes.SecretKeyFromED25519(ed25519.TestSecretKey([]byte("validator"))).Public(),
		Address:      address,
	}
	nodeKey := ed25519.TestSecretKey([]byte("validator node")).Public()
	require.NoError(t, validator.NodeKey.UnmarshalText([]byte("node:"+nodeKey.String())))
	fc := &tmcfg.AutobahnFileConfig{
		Validators:         []tmcfg.AutobahnValidator{validator},
		MaxTxsPerBlock:     5_000,
		MaxTxsPerSecond:    utils.None[uint64](),
		PersistentStateDir: filepath.Join("data", "autobahn"),
		DialInterval:       utils.Duration(10 * time.Second),
		EVMChainID:         testEVMChainID,
	}
	data, err := json.Marshal(fc)
	require.NoError(t, err)
	conf.AutobahnConfigFile = filepath.Join(conf.RootDir, "config", "autobahn.json")
	require.NoError(t, os.WriteFile(conf.AutobahnConfigFile, data, 0o600))

	tg, err := resolveTargetFromConfig(conf, configtest.AppOpts{gigaconfig.FlagStateSyncEnabled: true})
	require.NoError(t, err)
	giga := gigaconfig.DefaultConfig
	giga.StateSync.Enabled = true
	return fullnodeHome{conf: conf, giga: giga, genDoc: tg.genDoc, target: tg}
}

// startFullnode runs the node start of home up to its giga router and returns the BlockDB range
// and the app height.
func startFullnode(home fullnodeHome, t *testing.T) (utils.Option[blockstore.SuffixRange], int64, error) {
	t.Helper()
	return node.BuildFullnodeRouterForTesting(t.Context(), home.conf, home.giga, home.genDoc)
}

// startRestoredFullnode is startFullnode for a restored home, which must start and close cleanly.
func startRestoredFullnode(home fullnodeHome, t *testing.T) (utils.Option[blockstore.SuffixRange], int64) {
	t.Helper()
	status, appHeight, err := startFullnode(home, t)
	require.NoError(t, err)
	return status, appHeight
}

// seeded returns the BlockDB range of a home that holds only the records of road height.
func seeded(height int64) utils.Option[blockstore.SuffixRange] {
	road := atypes.RoadHeight(height) //nolint:gosec // test heights are positive.
	return utils.Some(blockstore.SuffixRange{First: road, NextBlock: road + 1, NextAppProposal: road + 1, NextAppQC: road + 1})
}

// TestARestoredFullnodeStartsAtTheCheckpoint imports an archive into a fullnode home and runs
// the node start of that home up to its giga router twice. The first start seeds road block H
// and removes the restore files; the second finds the seeded BlockDB and seeds nothing.
func TestARestoredFullnodeStartsAtTheCheckpoint(t *testing.T) {
	p := newProducer(t)
	p.commit(t, 1)
	h := p.commit(t, 2)
	home := newFullnodeHome(t)
	require.NoError(t, runImport(t.Context(), home.target, bytesSource(p.archive(t, testChainID, h)), io.Discard))

	for range 2 {
		status, appHeight := startRestoredFullnode(home, t)
		require.Equal(t, h, appHeight)
		require.Equal(t, seeded(h), status)
		require.NoFileExists(t, home.target.layout.RestoreMarkerFile)
		require.NoFileExists(t, home.target.layout.RestoreProofFile)
	}
}

func TestARestoredHomeDoesNotStartWithTheSwitchOff(t *testing.T) {
	p := newProducer(t)
	h := p.commit(t, 1)
	home := newFullnodeHome(t)
	require.NoError(t, runImport(t.Context(), home.target, bytesSource(p.archive(t, testChainID, h)), io.Discard))

	home.giga.StateSync.Enabled = false
	_, _, err := startFullnode(home, t)
	require.ErrorContains(t, err, "set [giga.state_sync] enabled = true")
	require.FileExists(t, home.target.layout.RestoreMarkerFile)
}

// TestAnInterruptedImportDoesNotStart stops an import after it stores the restore anchor and
// before it installs FlatKV. The start refuses the home and keeps the restore marker. A second
// import finishes the restore, and the start then seeds road block H.
func TestAnInterruptedImportDoesNotStart(t *testing.T) {
	p := newProducer(t)
	h := p.commit(t, 1)
	archive := p.archive(t, testChainID, h)
	home := newFullnodeHome(t)
	stopAt := func(s string) error {
		if s == importStepAnchorStored {
			return errStopped
		}
		return nil
	}
	require.ErrorIs(t, importWith(t.Context(), home.target, bytesSource(archive), io.Discard, stopAt), errStopped)

	_, _, err := startFullnode(home, t)
	require.ErrorContains(t, err, "is incomplete; run seid statesync import again: FlatKV is at 0")
	require.FileExists(t, home.target.layout.RestoreMarkerFile)

	require.NoError(t, runImport(t.Context(), home.target, bytesSource(archive), io.Discard))
	status, appHeight := startRestoredFullnode(home, t)
	require.Equal(t, h, appHeight)
	require.Equal(t, seeded(h), status)
	require.NoFileExists(t, home.target.layout.RestoreMarkerFile)
}

// TestAStartWithoutASeedImportsAgain runs a start on a restored home that lost its state sync
// proof file, so the start opens the storage and has nothing to seed the BlockDB from. The start
// keeps the restore marker, a second import replaces the data of both, and the start then seeds
// road block H.
func TestAStartWithoutASeedImportsAgain(t *testing.T) {
	p := newProducer(t)
	h := p.commit(t, 1)
	archive := p.archive(t, testChainID, h)
	home := newFullnodeHome(t)
	require.NoError(t, runImport(t.Context(), home.target, bytesSource(archive), io.Discard))
	require.NoError(t, os.Remove(home.target.layout.RestoreProofFile))

	_, _, err := startFullnode(home, t)
	require.ErrorContains(t, err, "this home holds no state sync proof file to seed it from; run seid statesync import again")
	require.FileExists(t, home.target.layout.RestoreMarkerFile)
	leftovers, err := foreignFiles(home.target.layout)
	require.NoError(t, err)
	require.NotEmpty(t, leftovers)

	require.NoError(t, runImport(t.Context(), home.target, bytesSource(archive), io.Discard))
	status, appHeight := startRestoredFullnode(home, t)
	require.Equal(t, h, appHeight)
	require.Equal(t, seeded(h), status)
	require.NoFileExists(t, home.target.layout.RestoreMarkerFile)
}

// writeAnchor replaces the app-hash vault of home with one that holds only the restore anchor of
// height with appHash.
func writeAnchor(t *testing.T, home fullnodeHome, height int64, appHash [32]byte) {
	t.Helper()
	tg := home.target
	require.NoError(t, os.RemoveAll(tg.layout.AppHashVaultDir))
	require.NoError(t, node.WriteRestoredAppHash(tg.layout, tg.mode, tg.storage, tg.genDoc, height, appHash))
}

// TestAStartRefusesARestoreAnchorThatTheAppQCDoesNotSign starts a restored home whose restore
// anchor is not the app hash that the AppQC of the state sync proof file signs. The start seeds
// the BlockDB, refuses the home and keeps the restore files. With the right anchor in place, the
// next start finds the seeded BlockDB and finishes the restore.
func TestAStartRefusesARestoreAnchorThatTheAppQCDoesNotSign(t *testing.T) {
	p := newProducer(t)
	h := p.commit(t, 1)
	home := newFullnodeHome(t)
	require.NoError(t, runImport(t.Context(), home.target, bytesSource(p.archive(t, testChainID, h)), io.Discard))
	writeAnchor(t, home, h, [32]byte{9})

	_, _, err := startFullnode(home, t)
	require.ErrorIs(t, err, node.ErrRestoreAnchorNotSigned)
	require.FileExists(t, home.target.layout.RestoreMarkerFile)
	require.FileExists(t, home.target.layout.RestoreProofFile)

	writeAnchor(t, home, h, fixtureAt(t, h).AppHash)
	status, appHeight := startRestoredFullnode(home, t)
	require.Equal(t, h, appHeight)
	require.Equal(t, seeded(h), status)
	require.NoFileExists(t, home.target.layout.RestoreMarkerFile)
	require.NoFileExists(t, home.target.layout.RestoreProofFile)
}

// TestAStartThatStoppedBeforeItRemovedTheRestoreFilesFinishes starts a restored home, and then
// puts back the restore files that a start which stopped early leaves. The next start finds the
// seeded BlockDB and removes them.
func TestAStartThatStoppedBeforeItRemovedTheRestoreFilesFinishes(t *testing.T) {
	for _, tc := range []struct {
		name string
		// keepProof is false when the earlier start also removed the state sync proof file.
		keepProof bool
	}{
		{name: "before the state sync proof file", keepProof: true},
		{name: "before the restore marker", keepProof: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newProducer(t)
			h := p.commit(t, 1)
			home := newFullnodeHome(t)
			require.NoError(t, runImport(t.Context(), home.target, bytesSource(p.archive(t, testChainID, h)), io.Discard))
			marker, err := os.ReadFile(home.target.layout.RestoreMarkerFile)
			require.NoError(t, err)
			startRestoredFullnode(home, t)

			require.NoError(t, os.WriteFile(home.target.layout.RestoreMarkerFile, marker, 0o600))
			if tc.keepProof {
				require.NoError(t, os.WriteFile(home.target.layout.RestoreProofFile, proofAt(t, h), 0o600))
			}
			status, appHeight := startRestoredFullnode(home, t)
			require.Equal(t, h, appHeight)
			require.Equal(t, seeded(h), status)
			require.NoFileExists(t, home.target.layout.RestoreMarkerFile)
			require.NoFileExists(t, home.target.layout.RestoreProofFile)
		})
	}
}

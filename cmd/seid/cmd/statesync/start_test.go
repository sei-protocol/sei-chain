package statesync

import (
	"encoding/json"
	"io"
	"net/url"
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
		EVMRPC:       tmcfg.URL{URL: utils.OrPanic1(url.Parse("ws://localhost:26660"))},
	}
	nodeKey := ed25519.TestSecretKey([]byte("validator node")).Public()
	require.NoError(t, validator.NodeKey.UnmarshalText([]byte("node:"+nodeKey.String())))
	fc := &tmcfg.AutobahnFileConfig{
		Validators:         []tmcfg.AutobahnValidator{validator},
		MaxTxsPerBlock:     5_000,
		MaxTxsPerSecond:    utils.None[uint64](),
		BlockInterval:      utils.Duration(400 * time.Millisecond),
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
	return node.StartFullnodeForTesting(t.Context(), home.conf, home.giga, home.genDoc)
}

// startRestoredFullnode is startFullnode for a restored home. A restored home holds no state WAL
// block until it executes the block after the checkpoint, so its storage close reports that the
// state WAL stands at block 0.
func startRestoredFullnode(home fullnodeHome, t *testing.T) (utils.Option[blockstore.SuffixRange], int64) {
	t.Helper()
	status, appHeight, err := startFullnode(home, t)
	require.ErrorContains(t, err, "the state WAL stands at block 0")
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

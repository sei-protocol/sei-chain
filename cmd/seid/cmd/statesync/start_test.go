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

// testGenesisTime is the genesis time of the test chain. A genesis file needs one that is not zero.
var testGenesisTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// validatorKey returns the key of the one genesis validator of the test chain.
func validatorKey() atypes.SecretKey {
	return atypes.SecretKeyFromED25519(ed25519.TestSecretKey([]byte("validator")))
}

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

	genDoc := &tmtypes.GenesisDoc{ChainID: testChainID, InitialHeight: 1, GenesisTime: testGenesisTime}
	require.NoError(t, os.MkdirAll(filepath.Dir(conf.GenesisFile()), 0o750))
	require.NoError(t, genDoc.SaveAs(conf.GenesisFile()))

	address, err := tcp.ParseHostPort("localhost:26660")
	require.NoError(t, err)
	validator := tmcfg.AutobahnValidator{
		ValidatorKey: validatorKey().Public(),
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
	loaded, err := tmtypes.GenesisDocFromFile(conf.GenesisFile())
	require.NoError(t, err)
	return fullnodeHome{conf: conf, giga: giga, genDoc: loaded, target: tg}
}

// signedArchive returns the archive of the checkpoint of p at height, with a state sync proof file
// that the genesis validator of home signs.
func signedArchive(t *testing.T, p *producer, home fullnodeHome, height int64) []byte {
	t.Helper()
	proof, err := node.StateSyncProofForTesting(home.genDoc, []atypes.SecretKey{validatorKey()}, height, utils.GenBytes(utils.TestRngFromSeed(height), 32))
	require.NoError(t, err)
	return archiveWithProof(t, p.stage(t, height), testChainID, height, proof)
}

// startRestoredFullnode runs the node start of home up to its giga router and returns the BlockDB
// range and the app height. A restored home holds no state WAL block until it executes the block
// after the checkpoint, so its storage close reports that the state WAL stands at block 0.
func startRestoredFullnode(t *testing.T, home fullnodeHome) (utils.Option[blockstore.SuffixRange], int64) {
	t.Helper()
	status, appHeight, err := node.StartFullnodeForTesting(t.Context(), home.conf, home.giga, home.genDoc)
	require.ErrorContains(t, err, "the state WAL stands at block 0")
	return status, appHeight
}

// TestARestoredFullnodeStartsAtTheCheckpoint imports an archive into a fullnode home and runs the
// node start of that home up to its giga router twice. The first start seeds road block H and
// removes the restore files; the second finds the seeded BlockDB and seeds nothing.
func TestARestoredFullnodeStartsAtTheCheckpoint(t *testing.T) {
	p := newProducer(t)
	p.commit(t, 1)
	h := p.commit(t, 2)
	home := newFullnodeHome(t)
	require.NoError(t, runImport(t.Context(), home.target, bytesSource(signedArchive(t, p, home, h)), io.Discard))

	road := atypes.RoadHeight(h) //nolint:gosec // test heights are positive.
	want := utils.Some(blockstore.SuffixRange{First: road, NextBlock: road + 1, NextAppProposal: road + 1, NextAppQC: road + 1})
	for range 2 {
		status, appHeight := startRestoredFullnode(t, home)
		require.Equal(t, h, appHeight)
		require.Equal(t, want, status)
		require.NoFileExists(t, home.target.layout.RestoreMarkerFile)
		require.NoFileExists(t, home.target.layout.RestoreProofFile)
	}
}

// TestAnInterruptedImportDoesNotStart stops an import after it stores the restore anchor and
// before it installs FlatKV. The start refuses the home and keeps the restore marker. A second
// import finishes the restore, and the start then seeds road block H.
func TestAnInterruptedImportDoesNotStart(t *testing.T) {
	p := newProducer(t)
	h := p.commit(t, 1)
	home := newFullnodeHome(t)
	archive := signedArchive(t, p, home, h)
	stopAt := func(s string) error {
		if s == importStepAnchorStored {
			return errStopped
		}
		return nil
	}
	require.ErrorIs(t, importWith(t.Context(), home.target, bytesSource(archive), io.Discard, stopAt), errStopped)

	_, _, err := node.StartFullnodeForTesting(t.Context(), home.conf, home.giga, home.genDoc)
	require.ErrorContains(t, err, "run seid statesync import again: FlatKV is at 0")
	require.FileExists(t, home.target.layout.RestoreMarkerFile)

	require.NoError(t, runImport(t.Context(), home.target, bytesSource(archive), io.Discard))
	status, appHeight := startRestoredFullnode(t, home)
	require.Equal(t, h, appHeight)
	road := atypes.RoadHeight(h) //nolint:gosec // test heights are positive.
	require.Equal(t, utils.Some(blockstore.SuffixRange{First: road, NextBlock: road + 1, NextAppProposal: road + 1, NextAppQC: road + 1}), status)
	require.NoFileExists(t, home.target.layout.RestoreMarkerFile)
}

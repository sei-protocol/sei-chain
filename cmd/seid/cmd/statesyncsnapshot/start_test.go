package statesyncsnapshot

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
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
	}
	data, err := json.Marshal(fc)
	require.NoError(t, err)
	conf.AutobahnConfigFile = filepath.Join(conf.RootDir, "config", "autobahn.json")
	require.NoError(t, os.WriteFile(conf.AutobahnConfigFile, data, 0o600))

	tg, err := resolveTargetFromConfig(conf, appOptions{gigaconfig.FlagStateSyncEnabled: true})
	require.NoError(t, err)
	lock, err := flatkv.LockRoot(tg.layout.FlatKVDir)
	require.NoError(t, err)
	require.NoError(t, lock.Unlock())

	giga := gigaconfig.DefaultConfig
	giga.StateSync.Enabled = true
	loaded, err := tmtypes.GenesisDocFromFile(conf.GenesisFile())
	require.NoError(t, err)
	return fullnodeHome{conf: conf, giga: giga, genDoc: loaded, target: tg}
}

// TestARestoredFullnodeStartsAtTheCheckpoint restores an archive into a fullnode home and runs
// the node start of that home up to its giga router twice. The first start seeds road block H
// and removes the restore files; the second finds the seeded BlockDB and seeds nothing.
func TestARestoredFullnodeStartsAtTheCheckpoint(t *testing.T) {
	p := newProducer(t, 0)
	p.commit(t, 1)
	p.commit(t, 2)
	archive, h := p.createArchive(t)
	home := newFullnodeHome(t)
	require.NoError(t, runRestore(t.Context(), home.target, archive, nil, &bytes.Buffer{}))

	road := atypes.RoadIndex(h) //nolint:gosec // test heights are positive.
	want := utils.Some(blockstore.SuffixRange{First: road, NextBlock: road + 1, NextAppProposal: road + 1, NextAppQC: road + 1})
	for range 2 {
		status, appHeight, err := node.StartFullnodeForTesting(t.Context(), home.conf, home.giga, home.genDoc)
		require.NoError(t, err)
		require.Equal(t, h, appHeight)
		require.Equal(t, want, status)
		requireMarker(t, home.target, utils.None[int64]())
		require.NoFileExists(t, home.target.layout.RestoreProofFile)
	}
}

func TestARestoredHomeDoesNotStartWithTheSwitchOff(t *testing.T) {
	p := newProducer(t, 0)
	p.commit(t, 1)
	archive, h := p.createArchive(t)
	home := newFullnodeHome(t)
	require.NoError(t, runRestore(t.Context(), home.target, archive, nil, &bytes.Buffer{}))

	home.giga.StateSync.Enabled = false
	_, _, err := node.StartFullnodeForTesting(t.Context(), home.conf, home.giga, home.genDoc)
	require.ErrorContains(t, err, "set [giga.state_sync] enabled = true")
	requireMarker(t, home.target, utils.Some(h))
}

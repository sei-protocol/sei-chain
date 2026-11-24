package statesync

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// commandHome returns the server context of a fullnode home of chain testChainID with giga state
// sync enabled, and the Autobahn layout of that home.
func commandHome(t *testing.T) (*server.Context, node.AutobahnLayout) {
	t.Helper()
	home := t.TempDir()
	cfg := tmcfg.DefaultConfig()
	cfg.SetRoot(home)
	cfg.Mode = tmcfg.ModeFull
	cfg.AutobahnConfigFile = writeAutobahnConfig(t, home)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg.GenesisFile()), 0o750))
	genDoc := &tmtypes.GenesisDoc{ChainID: testChainID, InitialHeight: 1, GenesisTime: time.Unix(1, 0).UTC()}
	require.NoError(t, genDoc.SaveAs(cfg.GenesisFile()))

	v := viper.New()
	v.Set("giga.state_sync.enabled", true)
	layout, err := node.ResolveAutobahnLayout(cfg)
	require.NoError(t, err)
	return server.NewContext(v, cfg), layout
}

// writeAutobahnConfig writes an Autobahn config of one validator, with the persistent state dir
// data/autobahn under home, and returns its path.
func writeAutobahnConfig(t *testing.T, home string) string {
	t.Helper()
	var validator tmcfg.AutobahnValidator
	validatorJSON, err := json.Marshal(map[string]string{
		"validator_key": atypes.TestSecretKey(atypes.NodeID("validator")).Public().String(),
		"node_key":      "node:" + ed25519.TestSecretKey([]byte("node")).Public().String(),
		"address":       "localhost:26660",
		"evmrpc":        "ws://localhost:26660",
	})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(validatorJSON, &validator))
	data, err := json.Marshal(&tmcfg.AutobahnFileConfig{
		Validators:         []tmcfg.AutobahnValidator{validator},
		MaxTxsPerBlock:     5_000,
		MaxTxsPerSecond:    utils.None[uint64](),
		BlockInterval:      utils.Duration(400 * time.Millisecond),
		PersistentStateDir: filepath.Join("data", "autobahn"),
		DialInterval:       utils.Duration(10 * time.Second),
		EVMChainID:         713700,
	})
	require.NoError(t, err)
	path := filepath.Join(home, "config", "autobahn.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

// runCmd runs `statesync args...` in the server context sctx and returns its error.
func runCmd(t *testing.T, sctx *server.Context, args ...string) error {
	t.Helper()
	cmd := Cmd()
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.ExecuteContext(context.WithValue(t.Context(), server.ServerContextKey, sctx))
}

func TestImportCommandImportsALocalArchive(t *testing.T) {
	p := newProducer(t)
	p.commit(t, 1)
	height := p.commit(t, 2)
	path := filepath.Join(t.TempDir(), "archive.tar")
	require.NoError(t, os.WriteFile(path, p.archive(t, testChainID, height), 0o600))
	sctx, layout := commandHome(t)

	require.NoError(t, runCmd(t, sctx, "import", "--from", path))

	requireRestored(t, target{layout: layout}, p, height)
}

func TestImportCommandRejects(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want error
	}{
		"a negative height":          {[]string{"import", "--height", "-1"}, errNegativeHeight},
		"a height with --from":       {[]string{"import", "--from", "archive.tar", "--height", "5"}, errHeightWithFrom},
		"a region with a local file": {[]string{"import", "--from", "archive.tar", "--region", "us-east-2"}, errRegionWithLocalFrom},
		"no source":                  {[]string{"import"}, errNoSource},
	} {
		t.Run(name, func(t *testing.T) {
			sctx, layout := commandHome(t)
			require.ErrorIs(t, runCmd(t, sctx, tc.args...), tc.want)
			requireUntouched(t, target{layout: layout})
		})
	}
}

func TestImportCommandRefusesWhenStateSyncIsDisabled(t *testing.T) {
	sctx, _ := commandHome(t)
	sctx.Viper.Set("giga.state_sync.enabled", false)

	require.ErrorIs(t, runCmd(t, sctx, "import", "--from", "archive.tar"), errDisabled)
}

func TestCheckCommandRejectsAHomeWithNoBucket(t *testing.T) {
	sctx, _ := commandHome(t)

	require.ErrorIs(t, runCmd(t, sctx, "check"), errNoBucket)
}

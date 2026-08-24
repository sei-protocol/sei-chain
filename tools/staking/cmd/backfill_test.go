package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

type testAppToml struct {
	srvconfig.Config

	StateCommit  seidbconfig.StateCommitConfig  `mapstructure:"state-commit"`
	StateStore   seidbconfig.StateStoreConfig   `mapstructure:"state-store"`
	ReceiptStore seidbconfig.ReceiptStoreConfig `mapstructure:"receipt-store"`
}

func writeTestAppToml(t *testing.T, path string) {
	t.Helper()

	cfg := testAppToml{
		Config:       *srvconfig.DefaultConfig(),
		StateCommit:  seidbconfig.DefaultStateCommitConfig(),
		StateStore:   seidbconfig.DefaultStateStoreConfig(),
		ReceiptStore: seidbconfig.DefaultReceiptStoreConfig(),
	}
	cfg.MinGasPrices = "0.01usei"
	cfg.StateCommit.Enable = true
	cfg.StateCommit.HashLogger.Enable = false
	cfg.StateStore.Enable = true

	template := srvconfig.ManualConfigTemplate +
		seidbconfig.StateCommitConfigTemplate +
		seidbconfig.StateStoreConfigTemplate +
		seidbconfig.ReceiptStoreConfigTemplate
	srvconfig.SetConfigTemplate(template)
	srvconfig.WriteConfigFile(path, cfg)
}

func seedCommittedSeiDBApp(t *testing.T, home string) int64 {
	t.Helper()

	encCfg := app.MakeEncodingConfig()
	seiApp := app.New(
		nil,
		nil,
		false,
		map[int64]bool{},
		home,
		1,
		true,
		tmcfg.TestConfig(),
		encCfg,
		app.GetWasmEnabledProposals(),
		app.TestAppOpts{UseSc: true},
		app.EmptyWasmOpts,
		app.EmptyAppOptions,
	)

	genesisState := app.NewDefaultGenesisState(encCfg.Marshaler)
	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	require.NoError(t, err)

	_, err = seiApp.InitChain(&abci.RequestInitChain{
		ConsensusParams: app.DefaultConsensusParams,
		ChainId:         "sei-test",
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	commitID := seiApp.CommitMultiStore().Commit(true)
	require.NoError(t, seiApp.Close())
	return commitID.Version
}

func backfillCmdWithHome(t *testing.T, home string, args ...string) *cobra.Command {
	t.Helper()

	cmd := BackfillDelegationIndexCmd()
	serverCtx := server.NewDefaultContext()
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, serverCtx))
	serverCtx.Viper.Set(flags.FlagHome, home)
	serverCtx.Viper.Set("chain-id", "sei-test")
	serverCtx.Viper.SetConfigFile(filepath.Join(home, "config", "app.toml"))
	require.NoError(t, serverCtx.Viper.ReadInConfig())

	cmd.SetArgs(args)
	return cmd
}

func captureCommandOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	stdoutR, stdoutW, err := os.Pipe()
	require.NoError(t, err)
	stderrR, stderrW, err := os.Pipe()
	require.NoError(t, err)

	oldStdout, oldStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutW, stderrW
	t.Cleanup(func() {
		os.Stdout, os.Stderr = oldStdout, oldStderr
	})

	fn()

	require.NoError(t, stdoutW.Close())
	require.NoError(t, stderrW.Close())
	os.Stdout, os.Stderr = oldStdout, oldStderr

	var stdoutBuf, stderrBuf bytes.Buffer
	_, err = io.Copy(&stdoutBuf, stdoutR)
	require.NoError(t, err)
	_, err = io.Copy(&stderrBuf, stderrR)
	require.NoError(t, err)
	return stdoutBuf.String(), stderrBuf.String()
}

func TestBackfillDelegationIndexDryRun(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o755))
	writeTestAppToml(t, filepath.Join(home, "config", "app.toml"))
	seedCommittedSeiDBApp(t, home)

	cmd := backfillCmdWithHome(t, home)
	stdout, _ := captureCommandOutput(t, func() {
		require.NoError(t, cmd.Execute())
	})

	require.Contains(t, stdout, "dry_run=true")
	require.NotContains(t, stdout, "committed_version=")
}

func TestBackfillDelegationIndexWrite(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o755))
	writeTestAppToml(t, filepath.Join(home, "config", "app.toml"))
	version := seedCommittedSeiDBApp(t, home)

	cmd := backfillCmdWithHome(t, home, "--write")
	stdout, stderr := captureCommandOutput(t, func() {
		require.NoError(t, cmd.Execute())
	})

	require.Contains(t, stdout, "dry_run=false")
	require.Contains(t, stdout, "committed_version=")
	require.Contains(t, stderr, "WARNING:")
	require.Greater(t, version, int64(0))
}

func TestBackfillDelegationIndexWriteRejectsHeight(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o755))
	writeTestAppToml(t, filepath.Join(home, "config", "app.toml"))

	cmd := backfillCmdWithHome(t, home, "--write", "--height", "1")
	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "--write cannot be used with --height")
}

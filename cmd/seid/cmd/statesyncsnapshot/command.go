// Package statesyncsnapshot implements `seid state-sync-snapshot`, which packs a node's FlatKV
// checkpoint and epoch registry into an archive, and restores one into a fresh Autobahn fullnode home.
package statesyncsnapshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// errDisabled is returned by both commands when [giga.state_sync] enabled is false.
var errDisabled = errors.New("giga state sync is disabled; set [giga.state_sync] enabled = true in app.toml")

// Cmd returns the `state-sync-snapshot` command group.
func Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state-sync-snapshot",
		Short: "Create or restore a Giga state sync archive",
	}
	cmd.AddCommand(createCmd(), restoreCmd())
	return cmd
}

func createCmd() *cobra.Command {
	var opts createOptions
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Pack the current FlatKV checkpoint and the epoch registry into an archive",
		Long: `Pack the current FlatKV checkpoint and the epoch registry of this home into an archive. It is
safe to run while the node runs. With --out -, it writes the archive to stdout and prints one
line {"height":H} on stderr before the first archive byte.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.out == "" {
				return errors.New("--out is required")
			}
			if opts.height < 0 {
				return fmt.Errorf("--height %d is negative", opts.height)
			}
			stdout := cmd.OutOrStdout()
			if opts.out == "-" {
				archive, err := takeStdout()
				if err != nil {
					return err
				}
				defer func() { _ = archive.Close() }()
				stdout = archive
			}
			t, err := resolveTarget(cmd)
			if err != nil {
				return err
			}
			return withSignals(cmd.Context(), func(ctx context.Context) error {
				return runCreate(ctx, t, opts, stdout, cmd.ErrOrStderr())
			})
		},
	}
	cmd.Flags().StringVar(&opts.out, "out", "", "archive path, or - for stdout")
	cmd.Flags().Int64Var(&opts.height, "height", 0, "checkpoint height to pack (default: the current checkpoint)")
	cmd.Flags().StringVar(&opts.stagingParent, "staging-dir", "",
		"dir for the staging files (default: the persistent state dir); it must be on the same filesystem as the FlatKV root")
	return cmd
}

func restoreCmd() *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore an archive into a fresh Autobahn fullnode home",
		Long: `Restore an archive into a fresh Autobahn fullnode home. If it fails or stops, run the same
command again. It never deletes Autobahn data that an earlier restore did not write.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if from == "" {
				return errors.New("--from is required")
			}
			t, err := resolveTarget(cmd)
			if err != nil {
				return err
			}
			return withSignals(cmd.Context(), func(ctx context.Context) error {
				return runRestore(ctx, t, from, cmd.InOrStdin(), cmd.ErrOrStderr())
			})
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "archive path, or - for stdin")
	return cmd
}

// target is the node home a command acts on.
type target struct {
	layout  node.AutobahnLayout
	chainID string
	mode    string
}

func resolveTarget(cmd *cobra.Command) (target, error) {
	sctx := server.GetServerContextFromCmd(cmd)
	return resolveTargetFromConfig(sctx.Config, sctx.Viper)
}

// resolveTargetFromConfig returns the home that cfg and the app options describe. It refuses before it
// creates any file when the home is not an Autobahn home or giga state sync is disabled.
func resolveTargetFromConfig(cfg *tmcfg.Config, opts gigaconfig.AppOptions) (target, error) {
	if cfg.AutobahnConfigFile == "" {
		return target{}, errors.New("autobahn-config-file is not set: state sync archives are for Autobahn nodes only")
	}
	gigaCfg, err := gigaconfig.ReadConfig(opts)
	if err != nil {
		return target{}, fmt.Errorf("read [giga] config: %w", err)
	}
	if !gigaCfg.StateSync.Enabled {
		return target{}, errDisabled
	}
	layout, err := node.ResolveAutobahnLayout(cfg, gigaCfg.Storage)
	if err != nil {
		return target{}, fmt.Errorf("resolve the Autobahn layout: %w", err)
	}
	genDoc, err := tmtypes.GenesisDocFromFile(cfg.GenesisFile())
	if err != nil {
		return target{}, fmt.Errorf("read genesis: %w", err)
	}
	return target{layout: layout, chainID: genDoc.ChainID, mode: cfg.Mode}, nil
}

// withSignals runs f with a context that SIGINT and SIGTERM cancel, so f can clean up.
func withSignals(parent context.Context, f func(ctx context.Context) error) error {
	if parent == nil {
		parent = context.Background()
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return f(ctx)
}

// takeStdout returns a private handle on standard output and points file descriptor 1 at standard
// error, so log lines that any logger writes to stdout cannot enter the archive.
func takeStdout() (*os.File, error) {
	fd, err := unix.Dup(int(os.Stdout.Fd())) //nolint:gosec // a file descriptor fits in an int.
	if err != nil {
		return nil, fmt.Errorf("dup stdout: %w", err)
	}
	if err := unix.Dup2(int(os.Stderr.Fd()), int(os.Stdout.Fd())); err != nil { //nolint:gosec // a file descriptor fits in an int.
		_ = unix.Close(fd)
		return nil, fmt.Errorf("point stdout at stderr: %w", err)
	}
	return os.NewFile(uintptr(fd), "archive"), nil //nolint:gosec // fd is a valid descriptor from Dup.
}

// printHeight writes the one machine-readable line {"height":H}.
func printHeight(w io.Writer, height int64) error {
	line, err := json.Marshal(struct {
		Height int64 `json:"height"`
	}{height})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", line)
	return err
}

// ctxReader fails reads once ctx is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// stepHook is called at named steps of create and restore. A non-nil error stops the command there.
type stepHook func(step string) error

func noStop(string) error { return nil }

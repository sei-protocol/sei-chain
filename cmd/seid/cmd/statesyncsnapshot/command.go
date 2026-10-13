// Package statesyncsnapshot implements `seid state-sync-snapshot`, which packs a
// node's FlatKV checkpoint and epoch registry into an archive and restores one
// into an empty Autobahn home.
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

	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

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
	var out string
	var height int64
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Pack the current FlatKV checkpoint and the epoch registry into an archive",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if out == "" {
				return errors.New("--out is required")
			}
			t, err := resolveTarget(cmd)
			if err != nil {
				return err
			}
			return withSignals(cmd.Context(), func(ctx context.Context) error {
				return runCreate(ctx, t, out, height, cmd.OutOrStdout(), cmd.ErrOrStderr())
			})
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "archive path, or - for stdout")
	cmd.Flags().Int64Var(&height, "height", 0, "checkpoint height to pack (default: the current checkpoint)")
	return cmd
}

func restoreCmd() *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore an archive into an empty Autobahn fullnode home",
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
	cfg := server.GetServerContextFromCmd(cmd).Config
	return resolveTargetFromConfig(cfg)
}

func resolveTargetFromConfig(cfg *tmcfg.Config) (target, error) {
	if cfg.AutobahnConfigFile == "" {
		return target{}, errors.New("autobahn-config-file is not set: state sync archives are for Autobahn nodes only")
	}
	layout, err := node.ResolveAutobahnLayout(cfg)
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

// printHeight writes the one machine-readable line `{"height":H}`.
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

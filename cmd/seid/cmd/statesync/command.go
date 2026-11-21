// Package statesync implements the `seid statesync` commands: import, which imports a giga state
// sync archive into a fresh Autobahn fullnode home, and check, which checks the access of a node to
// its archive bucket.
package statesync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	ssarchive "github.com/sei-protocol/sei-chain/sei-db/statesync"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// errDisabled is returned when [giga.state_sync] enabled is false.
var errDisabled = errors.New("giga state sync is disabled; set [giga.state_sync] enabled = true in app.toml")

// Errors that resolveSource returns for flags that do not select one archive.
var (
	errHeightWithFrom      = errors.New("--height selects an archive in snapshot_bucket and cannot be used with --from")
	errRegionWithLocalFrom = errors.New("--region selects the region of a bucket and cannot be used with a local --from")
	errNoSource            = errors.New("set --from, or [giga.state_sync] snapshot_bucket in app.toml")
	errNotS3URL            = errors.New("--from is not s3://<bucket>/<key>")
)

// Cmd returns the `statesync` command group.
func Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "statesync",
		Short: "Import giga state sync archives and check the archive bucket",
	}
	cmd.AddCommand(importCmd(), checkCmd())
	return cmd
}

func importCmd() *cobra.Command {
	var from, region string
	var height int64
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import a state sync archive into a fresh Autobahn fullnode home",
		Long: `Import a state sync archive into a fresh Autobahn fullnode home. Without --from, it
downloads the archive from [giga.state_sync] snapshot_bucket: at --height, or else at the height
in latest.txt. It writes every row of the archived checkpoint into a new FlatKV database.

The persistent state dir must hold no data, or only the data of an earlier import, which import
replaces: it deletes the earlier import before it downloads the new archive. If import fails or
stops, run the same command again. The persistent state dir and the FlatKV dir must be on one
filesystem, and import refuses when that filesystem has less free space than twice the archived
checkpoint.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if height < 0 {
				return fmt.Errorf("--height %d is negative", height)
			}
			t, err := resolveTarget(cmd)
			if err != nil {
				return err
			}
			return withSignals(cmd.Context(), func(ctx context.Context) error {
				src, err := resolveSource(ctx, t, from, height, region)
				if err != nil {
					return err
				}
				return runImport(ctx, t, src, cmd.ErrOrStderr())
			})
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "archive to import: s3://<bucket>/<key> or a local path (default: snapshot_bucket)")
	cmd.Flags().Int64Var(&height, "height", 0, "height of the archive in snapshot_bucket (default: the height in latest.txt)")
	cmd.Flags().StringVar(&region, "region", "", "AWS region of the bucket (default: snapshot_region, then the AWS SDK default)")
	return cmd
}

// target is the node home that import acts on.
type target struct {
	layout    node.AutobahnLayout
	chainID   string
	mode      string
	genDoc    *tmtypes.GenesisDoc
	storage   gigaconfig.StorageConfig
	stateSync gigaconfig.StateSyncConfig
}

func resolveTarget(cmd *cobra.Command) (target, error) {
	sctx := server.GetServerContextFromCmd(cmd)
	return resolveTargetFromConfig(sctx.Config, sctx.Viper)
}

// resolveTargetFromConfig returns the home that cfg and the app options describe. It refuses
// before it creates any file when the home is not an Autobahn home or giga state sync is disabled.
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
	genDoc, err := tmtypes.GenesisDocFromFile(cfg.GenesisFile())
	if err != nil {
		return target{}, fmt.Errorf("read genesis: %w", err)
	}
	layout, err := node.ResolveAutobahnLayout(cfg)
	if err != nil {
		return target{}, fmt.Errorf("resolve the Autobahn layout: %w", err)
	}
	return target{
		layout:    layout,
		chainID:   genDoc.ChainID,
		mode:      cfg.Mode,
		genDoc:    genDoc,
		storage:   gigaCfg.Storage,
		stateSync: gigaCfg.StateSync,
	}, nil
}

// source is an archive that import reads.
type source struct {
	name string
	// height is the height that the archive must have, or 0 when any height is accepted.
	height int64
	open   func(ctx context.Context) (io.ReadCloser, error)
}

// resolveSource returns the archive at from, or when from is empty, the archive at height in the
// archive bucket of t, or at the height in its latest.txt when height is 0. A bucket is in region,
// or in the archive region of t when region is empty.
func resolveSource(ctx context.Context, t target, from string, height int64, region string) (source, error) {
	if from != "" && height != 0 {
		return source{}, errHeightWithFrom
	}
	if from != "" && !strings.HasPrefix(from, "s3://") && region != "" {
		return source{}, errRegionWithLocalFrom
	}
	if region == "" {
		region = t.stateSync.SnapshotRegion
	}
	if from == "" {
		if t.stateSync.SnapshotBucket == "" {
			return source{}, errNoSource
		}
		store, err := ssarchive.NewS3Store(ctx, t.stateSync.SnapshotBucket, region)
		if err != nil {
			return source{}, err
		}
		return bucketSource(ctx, store, t.chainID, height)
	}
	if rest, ok := strings.CutPrefix(from, "s3://"); ok {
		bucket, key, ok := strings.Cut(rest, "/")
		if !ok || bucket == "" || key == "" {
			return source{}, fmt.Errorf("%w: %q", errNotS3URL, from)
		}
		store, err := ssarchive.NewS3Store(ctx, bucket, region)
		if err != nil {
			return source{}, err
		}
		return objectSource(from, store, key, 0), nil
	}
	return source{
		name: from,
		open: func(context.Context) (io.ReadCloser, error) {
			return os.Open(from) //nolint:gosec // from is the operator's path.
		},
	}, nil
}

// bucketSource returns the archive of chain chainID at height in store, or at the height in its
// latest.txt when height is 0.
func bucketSource(ctx context.Context, store ssarchive.ObjectStore, chainID string, height int64) (source, error) {
	if height == 0 {
		latest, err := ssarchive.ReadLatest(ctx, store, chainID)
		if err != nil {
			return source{}, fmt.Errorf("read the newest archive height: %w", err)
		}
		height = latest
	}
	key := ssarchive.ArchiveKey(chainID, height)
	return objectSource(key, store, key, height), nil
}

func objectSource(name string, store ssarchive.ObjectStore, key string, height int64) source {
	return source{
		name:   name,
		height: height,
		open: func(ctx context.Context) (io.ReadCloser, error) {
			body, _, err := store.Get(ctx, key)
			return body, err
		},
	}
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

// stepHook is called at named steps of import. A non-nil error stops import there.
type stepHook func(step string) error

func noStop(string) error { return nil }

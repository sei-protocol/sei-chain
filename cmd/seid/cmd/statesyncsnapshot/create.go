package statesyncsnapshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

const (
	createStagingDirName = ".state-sync-create"
	linkAttempts         = 3
	// registryMargin covers the gap between the commit of a checkpoint and the mtime of its directory,
	// which the store writes asynchronously.
	registryMargin = time.Minute
)

// Steps of create at which a stepHook is called.
const (
	createStepStaged   = "create: staging ready"
	createStepLinked   = "create: checkpoint linked"
	createStepManifest = "create: manifest built"
	createStepWritten  = "create: archive written"
)

// createOptions are the flags of create.
type createOptions struct {
	out           string
	height        int64
	stagingParent string
}

// runCreate packs a checkpoint and the epoch registry of t into an archive at opts.out, or into stdout
// when opts.out is "-".
func runCreate(ctx context.Context, t target, opts createOptions, stdout, stderr io.Writer) error {
	return createWith(ctx, t, opts, stdout, stderr, noStop)
}

// createWith is runCreate with a hook that can stop it at each step.
func createWith(ctx context.Context, t target, opts createOptions, stdout, stderr io.Writer, stop stepHook) error {
	parent := opts.stagingParent
	if parent == "" {
		parent = t.layout.PersistentStateDir
	}
	unlock, err := lockCreate(parent)
	if err != nil {
		return err
	}
	defer unlock()

	stagingRoot := filepath.Join(parent, createStagingDirName)
	if err := removeCreateLeftovers(stagingRoot, opts.out); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stagingRoot) }()
	stage := filepath.Join(stagingRoot, "create")
	if err := os.MkdirAll(filepath.Join(stage, flatkvPrefix), 0o750); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	if err := stop(createStepStaged); err != nil {
		return err
	}

	version, written, err := linkCheckpoint(t.layout.FlatKVDir, opts.height, filepath.Join(stage, flatkvPrefix))
	if err != nil {
		return err
	}
	if err := flatkv.VerifySnapshotDir(filepath.Join(stage, flatkvPrefix, flatkv.SnapshotDirName(version)), version); err != nil {
		return fmt.Errorf("verify the linked checkpoint: %w", err)
	}
	if err := stop(createStepLinked); err != nil {
		return err
	}
	if err := copyEpochFiles(t.layout.EpochRegistryFiles, filepath.Join(stage, epochsPrefix)); err != nil {
		return err
	}
	if err := requireRegistryCoversCheckpoint(t.layout.EpochRegistryFiles, version, written); err != nil {
		return err
	}

	sources, err := collectSources(stage)
	if err != nil {
		return err
	}
	m, err := buildManifest(ctx, t.chainID, version, sources)
	if err != nil {
		return err
	}
	if err := stop(createStepManifest); err != nil {
		return err
	}
	if opts.out == "-" {
		if err := printHeight(stderr, version); err != nil {
			return err
		}
		if err := writeArchive(ctx, stdout, m, sources); err != nil {
			return err
		}
		return stop(createStepWritten)
	}
	if err := writeArchiveFile(ctx, opts.out, m, sources, stop); err != nil {
		return err
	}
	return printHeight(stderr, version)
}

// lockCreate takes an exclusive lock on dir until the returned function is called. It fails at once
// when another create holds the lock.
func lockCreate(dir string) (func(), error) {
	f, err := os.Open(dir) //nolint:gosec // dir is the operator's staging parent.
	if err != nil {
		return nil, fmt.Errorf("open staging parent: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil { //nolint:gosec // a file descriptor fits in an int.
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, fmt.Errorf("another state-sync-snapshot create is running with staging under %s", dir)
		}
		return nil, fmt.Errorf("lock %s: %w", dir, err)
	}
	return func() { _ = f.Close() }, nil
}

// removeCreateLeftovers removes the staging root and the partial archive that a create which stopped
// before its cleanup left. Their hard links keep checkpoint files on disk after the node prunes them.
func removeCreateLeftovers(stagingRoot, out string) error {
	if err := os.RemoveAll(stagingRoot); err != nil {
		return fmt.Errorf("remove the staging files of an earlier create: %w", err)
	}
	if out == "-" {
		return nil
	}
	if err := os.Remove(out + ".partial"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove the partial archive of an earlier create: %w", err)
	}
	return nil
}

// linkCheckpoint hard-links the checkpoint at height, or the current one when height is 0, into dir.
// It returns the checkpoint's version and the mtime of its directory in root.
func linkCheckpoint(root string, height int64, dir string) (int64, time.Time, error) {
	var lastErr error
	for range linkAttempts {
		version := height
		if version == 0 {
			v, err := flatkv.ActiveSnapshotVersion(root)
			if err != nil {
				return 0, time.Time{}, fmt.Errorf("find the current checkpoint (has the node written one yet?): %w", err)
			}
			version = v
		}
		written, err := linkOne(root, version, filepath.Join(dir, flatkv.SnapshotDirName(version)))
		if err == nil {
			return version, written, nil
		}
		lastErr = err
		// The node replaces the current checkpoint while it runs, so a pruned one is retried.
		if height != 0 || !errors.Is(err, os.ErrNotExist) {
			break
		}
	}
	return 0, time.Time{}, fmt.Errorf("link the checkpoint: %w", lastErr)
}

func linkOne(root string, version int64, dst string) (time.Time, error) {
	if err := flatkv.HardLinkSnapshot(root, version, dst); err != nil {
		return time.Time{}, err
	}
	info, err := os.Stat(filepath.Join(root, flatkv.SnapshotDirName(version)))
	if err != nil {
		_ = os.RemoveAll(dst)
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// copyEpochFiles byte-copies the epoch registry files into dst. The registry rewrites them in place,
// so a hard link would not keep their content.
func copyEpochFiles(files []string, dst string) error {
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return err
	}
	for _, p := range files {
		data, err := os.ReadFile(p) //nolint:gosec // p is a path of the node layout.
		if err != nil {
			return fmt.Errorf("read epoch file: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dst, filepath.Base(p)), data, 0o600); err != nil { //nolint:gosec // dst is the staging dir and p a path of the node layout.
			return fmt.Errorf("copy epoch file: %w", err)
		}
	}
	return nil
}

// requireRegistryCoversCheckpoint returns an error if a non-empty epoch registry file was written later
// than registryMargin before the checkpoint was. Such a registry can have pruned the checkpoint's epoch,
// and a node restored from it cannot verify the CommitQC at the checkpoint height.
func requireRegistryCoversCheckpoint(files []string, version int64, checkpointWritten time.Time) error {
	for _, p := range files {
		info, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("stat epoch file: %w", err)
		}
		if info.Size() == 0 || !info.ModTime().After(checkpointWritten.Add(-registryMargin)) {
			continue
		}
		return fmt.Errorf("the epoch registry changed after checkpoint %d (%s at %s, checkpoint at %s); wait for the next checkpoint",
			version, filepath.Base(p), info.ModTime().UTC().Format(time.RFC3339), checkpointWritten.UTC().Format(time.RFC3339))
	}
	return nil
}

// writeArchiveFile writes the archive to out.partial, fsyncs it, and renames it over out.
func writeArchiveFile(ctx context.Context, out string, m *manifest, sources []source, stop stepHook) (err error) {
	partial := out + ".partial"
	f, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // out is the operator's path.
	if err != nil {
		return fmt.Errorf("create %s: %w", partial, err)
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(partial)
		}
	}()
	if err := writeArchive(ctx, f, m, sources); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := stop(createStepWritten); err != nil {
		return err
	}
	if err := os.Rename(partial, out); err != nil {
		return err
	}
	return syncPath(filepath.Dir(out))
}

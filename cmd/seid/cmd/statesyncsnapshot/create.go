package statesyncsnapshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/sys/unix"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
)

const (
	createStagingDirName = ".state-sync-create"
	linkAttempts         = 3
)

var (
	// errCreateRunning is returned when another create holds a dir that this create needs.
	errCreateRunning = errors.New("another state-sync-snapshot create is running")
	// errNoProofFile is returned when the node has not written the state sync proof file of a
	// checkpoint that create would pack.
	errNoProofFile = errors.New("the node has written no state sync proof file (is [giga.state_sync] enabled on it?)")
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

// runCreate packs a checkpoint of t into an archive at opts.out, or into stdout when opts.out is "-".
func runCreate(ctx context.Context, t target, opts createOptions, stdout, stderr io.Writer) error {
	return createWith(ctx, t, opts, stdout, stderr, noStop)
}

// createWith is runCreate with a hook that can stop it at each step.
func createWith(ctx context.Context, t target, opts createOptions, stdout, stderr io.Writer, stop stepHook) error {
	parent := opts.stagingParent
	if parent == "" {
		parent = t.layout.PersistentStateDir
	}
	unlock, err := lockCreateDirs(parent, opts.out)
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
	if err := os.MkdirAll(stage, 0o750); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	if err := stop(createStepStaged); err != nil {
		return err
	}

	staged, err := stageCheckpoint(t.layout, opts.height, stage)
	if err != nil {
		return err
	}
	if err := flatkv.VerifySnapshotDir(staged.checkpoint, staged.version); err != nil {
		return fmt.Errorf("verify the linked checkpoint: %w", err)
	}
	if err := stop(createStepLinked); err != nil {
		return err
	}

	m, err := buildManifest(t.chainID, staged.version, staged.stats)
	if err != nil {
		return err
	}
	if err := stop(createStepManifest); err != nil {
		return err
	}
	if opts.out == "-" {
		if err := printHeight(stderr, staged.version); err != nil {
			return err
		}
		if err := writeArchive(ctx, stdout, m, staged.proof, staged.checkpoint); err != nil {
			return err
		}
		return stop(createStepWritten)
	}
	if err := writeArchiveFile(ctx, opts.out, m, staged.proof, staged.checkpoint, stop); err != nil {
		return err
	}
	return printHeight(stderr, staged.version)
}

// lockCreateDirs takes the create lock on the staging parent and, unless out is "-", on the dir of
// out, until the returned function is called.
func lockCreateDirs(stagingParent, out string) (func(), error) {
	unlockStaging, err := lockCreate(stagingParent)
	if err != nil {
		return nil, err
	}
	if out == "-" {
		return unlockStaging, nil
	}
	outDir := filepath.Dir(out)
	same, err := sameDir(stagingParent, outDir)
	if err != nil {
		unlockStaging()
		return nil, err
	}
	if same {
		return unlockStaging, nil
	}
	unlockOut, err := lockCreate(outDir)
	if err != nil {
		unlockStaging()
		return nil, err
	}
	return func() { unlockOut(); unlockStaging() }, nil
}

// lockCreate takes an exclusive lock on dir until the returned function is called. It fails at once
// with errCreateRunning when another create holds the lock.
func lockCreate(dir string) (func(), error) {
	f, err := os.Open(dir) //nolint:gosec // dir is the operator's staging parent or output dir.
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dir, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil { //nolint:gosec // a file descriptor fits in an int.
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w on %s", errCreateRunning, dir)
		}
		return nil, fmt.Errorf("lock %s: %w", dir, err)
	}
	return func() { _ = f.Close() }, nil
}

// sameDir reports whether a and b are the same dir.
func sameDir(a, b string) (bool, error) {
	ai, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(ai, bi), nil
}

// removeCreateLeftovers removes the staging root and the partial archive that a create which stopped
// before its cleanup left.
func removeCreateLeftovers(stagingRoot, out string) error {
	// The staging hard links keep pruned checkpoint files on disk until they are removed.
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

type stagedCheckpoint struct {
	version    int64
	checkpoint string
	proof      []byte
	stats      flatkv.SnapshotStats
}

// stageCheckpoint stages into stage the checkpoint at height, or the newest checkpoint
// that has a state sync proof file when height is 0.
func stageCheckpoint(layout node.AutobahnLayout, height int64, stage string) (stagedCheckpoint, error) {
	var lastErr error
	for range linkAttempts {
		version, err := chooseCheckpoint(layout, height)
		if err != nil {
			return stagedCheckpoint{}, err
		}
		staged, err := stageVersion(layout, version, stage)
		if err == nil {
			return staged, nil
		}
		lastErr = err
		// The node prunes a checkpoint and its proof file while it runs, so a pruned one is retried.
		if height != 0 || !errors.Is(err, os.ErrNotExist) {
			break
		}
		if err := os.RemoveAll(filepath.Join(stage, flatkvPrefix, flatkv.SnapshotDirName(version))); err != nil {
			return stagedCheckpoint{}, fmt.Errorf("remove the staged checkpoint: %w", err)
		}
	}
	return stagedCheckpoint{}, lastErr
}

// chooseCheckpoint returns height when the node wrote a state sync proof file for it, or the
// newest checkpoint with a state sync proof file when height is 0.
func chooseCheckpoint(layout node.AutobahnLayout, height int64) (int64, error) {
	if height != 0 {
		ok, err := hasProofFile(layout, height)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, fmt.Errorf("%w for height %d in %s", errNoProofFile, height, layout.StateSyncProofDir)
		}
		return height, nil
	}
	versions, err := flatkv.SnapshotVersions(layout.FlatKVDir)
	if err != nil {
		return 0, fmt.Errorf("list the checkpoints: %w", err)
	}
	for _, v := range slices.Backward(versions) {
		ok, err := hasProofFile(layout, v)
		if err != nil {
			return 0, err
		}
		if ok {
			return v, nil
		}
	}
	return 0, fmt.Errorf("%w for any of the %d checkpoints in %s", errNoProofFile, len(versions), layout.FlatKVDir)
}

// hasProofFile reports whether the node wrote the state sync proof file for height.
func hasProofFile(layout node.AutobahnLayout, height int64) (bool, error) {
	_, err := os.Stat(proofFile(layout, height))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat the state sync proof file: %w", err)
	}
	return true, nil
}

// proofFile returns the path of the state sync proof file that the node writes for height.
func proofFile(layout node.AutobahnLayout, height int64) string {
	return filepath.Join(layout.StateSyncProofDir, statesyncproof.FileName(atypes.RoadIndex(height))) //nolint:gosec // heights are positive.
}

// stageVersion hard-links the checkpoint at version into stage and reads its state sync proof file.
func stageVersion(layout node.AutobahnLayout, version int64, stage string) (stagedCheckpoint, error) {
	if err := os.MkdirAll(filepath.Join(stage, flatkvPrefix), 0o750); err != nil {
		return stagedCheckpoint{}, fmt.Errorf("create the staged flatkv dir: %w", err)
	}
	checkpoint := filepath.Join(stage, flatkvPrefix, flatkv.SnapshotDirName(version))
	if err := flatkv.HardLinkSnapshot(layout.FlatKVDir, version, checkpoint); err != nil {
		return stagedCheckpoint{}, fmt.Errorf("link the checkpoint: %w", err)
	}
	data, err := os.ReadFile(proofFile(layout, version))
	if err != nil {
		return stagedCheckpoint{}, fmt.Errorf("read the state sync proof file: %w", err)
	}
	if _, err := statesyncproof.Decode(data, atypes.RoadIndex(version)); err != nil { //nolint:gosec // heights are positive.
		return stagedCheckpoint{}, fmt.Errorf("decode the state sync proof file of height %d: %w", version, err)
	}
	stats, err := flatkv.SnapshotDirStats(checkpoint, version)
	if err != nil {
		return stagedCheckpoint{}, fmt.Errorf("read the checkpoint metadata: %w", err)
	}
	return stagedCheckpoint{version: version, checkpoint: checkpoint, proof: data, stats: stats}, nil
}

// writeArchiveFile writes the archive to out.partial, fsyncs it, and renames it over out.
func writeArchiveFile(
	ctx context.Context,
	out string,
	m *manifest,
	proof []byte,
	checkpoint string,
	stop stepHook,
) (err error) {
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
	if err := writeArchive(ctx, f, m, proof, checkpoint); err != nil {
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

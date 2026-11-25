package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/statesync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

const (
	// stateSyncUploadSweepInterval is the time between two state sync archive upload sweeps.
	stateSyncUploadSweepInterval = 30 * time.Second
	// stateSyncUploadMaxBackoff is the longest time between two sweeps after failed sweeps.
	stateSyncUploadMaxBackoff = 30 * time.Minute
	// stateSyncUploadClaimTimeout is the age at which another node may take over the upload claim
	// of a round. An upload that takes longer can be repeated by that node.
	stateSyncUploadClaimTimeout = time.Hour
	// stateSyncUploadStagingDirName names the staging dir under the FlatKV root. It is not a
	// snapshot dir name, so FlatKV does not load it as a snapshot.
	stateSyncUploadStagingDirName = ".state-sync-upload"
)

// errUploadEnded stops an archive writer whose upload returned before it read the whole archive.
var errUploadEnded = errors.New("upload ended")

// errCheckpointPruned is the error of a checkpoint, or its state sync proof file, that FlatKV
// removed before the upload staged it.
var errCheckpointPruned = errors.New("checkpoint pruned")

// stateSyncUploadStagingDir returns the dir under the FlatKV root flatKVDir that holds the
// hard-linked checkpoint during an upload.
func stateSyncUploadStagingDir(flatKVDir string) string {
	// Hard links need the staging dir on the file system of the checkpoint, and flatKVDir can be
	// its own mount.
	return filepath.Join(flatKVDir, stateSyncUploadStagingDirName)
}

// removeStateSyncUploadStaging removes the staging dir of an upload that did not complete.
func removeStateSyncUploadStaging(flatKVDir string) error {
	if err := os.RemoveAll(stateSyncUploadStagingDir(flatKVDir)); err != nil {
		return fmt.Errorf("remove state sync upload staging: %w", err)
	}
	return nil
}

// StateSyncUploadConfig is the state sync archive upload of one node.
type StateSyncUploadConfig struct {
	// Store receives the archives.
	Store statesync.ObjectStore
	// ChainID is the chain of the archives.
	ChainID string
	// NodeID names this node in the upload claim of the chain.
	NodeID string
	// Interval is the least time between two archive uploads of the chain to Store, by any node.
	Interval time.Duration
}

// StartStateSyncUpload starts uploading to cfg.Store a state sync archive of the newest retained
// FlatKV checkpoint that has a state sync proof file. Close stops it. It returns an error when cfg
// names no chain or node, or when the upload already started or the manager is closed.
func (m *GigaStorageManager) StartStateSyncUpload(cfg StateSyncUploadConfig) error {
	if cfg.ChainID == "" {
		return errors.New("state sync upload: chain ID is empty")
	}
	if cfg.NodeID == "" {
		return errors.New("state sync upload: node ID is empty")
	}
	uploader := newStateSyncUploader(cfg, m.cfg.FlatKVConfig.DataDir, dbutils.GetStateSyncProofPath(m.cfg.HomePath))
	return m.stateSyncUpload.start(func(ctx context.Context) {
		uploader.run(ctx, stateSyncUploadSweepInterval)
	})
}

// stateSyncUpload holds the upload loop of a GigaStorageManager. It starts at most once, and not
// after stop.
type stateSyncUpload struct {
	mu sync.Mutex
	// cancel is nil before start and after stop.
	cancel  context.CancelFunc
	done    chan struct{}
	stopped bool
}

// start runs run in a new goroutine with a context that stop cancels. It returns an error when a
// loop already started or stop ran.
func (u *stateSyncUpload) start(run func(ctx context.Context)) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.stopped {
		return errors.New("state sync upload: storage is closed")
	}
	if u.cancel != nil {
		return errors.New("state sync upload already started")
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	u.done = make(chan struct{})
	go func() {
		defer close(u.done)
		run(ctx)
	}()
	return nil
}

// stop cancels the loop, if any, and waits for it to exit. A later start fails. It is idempotent.
func (u *stateSyncUpload) stop() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.stopped = true
	if u.cancel != nil {
		u.cancel()
		<-u.done
		u.cancel = nil
	}
}

// stateSyncUploader uploads state sync archives of the FlatKV checkpoints of one home.
type stateSyncUploader struct {
	store     statesync.ObjectStore
	chainID   string
	owner     string
	interval  time.Duration
	flatKVDir string
	proofDir  string
	// stagingDir holds the hard-linked checkpoint during an upload. It is under flatKVDir.
	stagingDir string
	now        func() time.Time
	metrics    *stateSyncUploadMetrics
	// uploaded is the newest archive height known to be in store, or 0.
	uploaded int64
}

func newStateSyncUploader(cfg StateSyncUploadConfig, flatKVDir, proofDir string) *stateSyncUploader {
	return &stateSyncUploader{
		store:      cfg.Store,
		chainID:    cfg.ChainID,
		owner:      cfg.NodeID,
		interval:   cfg.Interval,
		flatKVDir:  flatKVDir,
		proofDir:   proofDir,
		stagingDir: stateSyncUploadStagingDir(flatKVDir),
		now:        time.Now,
		metrics:    defaultStateSyncUploadMetrics(),
	}
}

// run logs whether the store answers, then sweeps at once and again after each sweepDelay until ctx
// ends.
func (u *stateSyncUploader) run(ctx context.Context, interval time.Duration) {
	u.logStoreAccess(ctx)
	lastErr := ""
	failures := 0
	for {
		lastErr = u.sweepLogged(ctx, lastErr)
		if lastErr == "" {
			failures = 0
		} else {
			failures++
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(sweepDelay(interval, failures)):
		}
	}
}

// sweepDelay returns the time before the next sweep after failures failed sweeps in a row: interval,
// doubled for each failure, and at most stateSyncUploadMaxBackoff.
func sweepDelay(interval time.Duration, failures int) time.Duration {
	delay := interval
	for range failures {
		delay *= 2
		if delay >= stateSyncUploadMaxBackoff {
			return stateSyncUploadMaxBackoff
		}
	}
	return delay
}

// logStoreAccess reads latest.txt once, logs the result and records it. A failure does not stop the
// loop.
func (u *stateSyncUploader) logStoreAccess(ctx context.Context) {
	latest, info, err := statesync.ReadLatestInfo(ctx, u.store, u.chainID)
	switch {
	case errors.Is(err, statesync.ErrObjectNotFound):
		logger.Info("state sync archive store is readable and has no archive of this chain")
	case err != nil:
		if ctx.Err() == nil {
			logger.Error("state sync archive store is not readable", "err", err)
		}
	default:
		u.metrics.recordLatest(ctx, latest, info.Modified)
		logger.Info("state sync archive store is readable", "latest", latest)
	}
}

// sweepLogged runs one sweep and returns its error text, or "" on success. It counts each failed
// sweep, and logs the error only when the text differs from lastErr, the text of the previous sweep.
func (u *stateSyncUploader) sweepLogged(ctx context.Context, lastErr string) string {
	err := u.sweep(ctx)
	if err == nil || ctx.Err() != nil {
		return ""
	}
	u.metrics.recordFailure(ctx)
	if err.Error() != lastErr {
		logger.Error("state sync archive upload failed; retrying on next sweep", "err", err)
	}
	return err.Error()
}

// sweep uploads the archive of the newest retained checkpoint that has a state sync proof file. It
// does nothing when the store already has an archive at that height or above, when the last
// archive upload of the chain is less than the interval ago, or when another node holds the upload
// claim. It records that checkpoint, and the newest archive of the chain that it reads or writes.
func (u *stateSyncUploader) sweep(ctx context.Context) error {
	height, ok, err := u.newestCheckpointWithProof()
	if err != nil || !ok {
		return err
	}
	u.metrics.recordCheckpoint(ctx, height)
	if height <= u.uploaded {
		return nil
	}
	latest, info, err := statesync.ReadLatestInfo(ctx, u.store, u.chainID)
	if err == nil {
		u.metrics.recordLatest(ctx, latest, info.Modified)
	}
	switch {
	case errors.Is(err, statesync.ErrObjectNotFound):
	case err != nil:
		return err
	case latest >= height:
		u.uploaded = latest
		return nil
	case u.now().Sub(info.Modified) < u.interval:
		return nil
	}
	claimed, err := statesync.ClaimUpload(ctx, u.store, u.chainID, latest, u.owner, u.now(), stateSyncUploadClaimTimeout)
	if err != nil || !claimed {
		return err
	}
	newest, err := u.upload(ctx, height)
	if err != nil {
		return err
	}
	// A newer height is from another node, and the next sweep that reads latest.txt records it.
	if newest == height {
		u.metrics.recordLatest(ctx, height, u.now())
	}
	u.uploaded = max(u.uploaded, newest)
	return nil
}

// newestCheckpointWithProof returns the height of the newest retained checkpoint that has a state
// sync proof file, and false when there is none.
func (u *stateSyncUploader) newestCheckpointWithProof() (int64, bool, error) {
	versions, err := flatkv.SnapshotVersions(u.flatKVDir)
	if err != nil {
		return 0, false, err
	}
	for i := len(versions) - 1; i >= 0; i-- {
		if versions[i] < 1 {
			continue
		}
		_, err := os.Stat(u.proofPath(versions[i]))
		if err == nil {
			return versions[i], true, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return 0, false, err
		}
	}
	return 0, false, nil
}

func (u *stateSyncUploader) proofPath(height int64) string {
	return filepath.Join(u.proofDir, statesyncproof.FileName(types.RoadHeight(height))) //nolint:gosec // height is positive.
}

// upload uploads the archive of the checkpoint at height, unless the store already has it, then
// advances latest.txt to height. It returns the newest archive height in the store, or 0 when
// FlatKV pruned the checkpoint first.
func (u *stateSyncUploader) upload(ctx context.Context, height int64) (newest int64, err error) {
	present, err := u.hasArchive(ctx, height)
	if err != nil {
		return 0, err
	}
	if present {
		logger.Info("state sync archive is already in the store", "height", height)
		return statesync.AdvanceLatest(ctx, u.store, u.chainID, height)
	}
	start := time.Now()
	defer func() {
		if rmErr := os.RemoveAll(u.stagingDir); rmErr != nil {
			err = errors.Join(err, fmt.Errorf("remove upload staging: %w", rmErr))
		}
	}()
	proof, err := u.stage(height)
	if errors.Is(err, errCheckpointPruned) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	m, err := statesync.BuildManifest(u.chainID, height, u.stagingDir)
	if err != nil {
		return 0, err
	}
	size, err := statesync.ArchiveSize(m, int64(len(proof)))
	if err != nil {
		return 0, err
	}
	if err := u.putArchive(ctx, m, proof, size); err != nil {
		return 0, err
	}
	newest, err = statesync.AdvanceLatest(ctx, u.store, u.chainID, height)
	if err != nil {
		return 0, err
	}
	duration := time.Since(start)
	u.metrics.recordUpload(ctx, size, duration)
	logger.Info("uploaded state sync archive", "height", height, "bytes", size, "rows", m.RowCount,
		"duration", duration)
	return newest, nil
}

// hasArchive reports whether the store has the archive of the checkpoint at height.
func (u *stateSyncUploader) hasArchive(ctx context.Context, height int64) (bool, error) {
	_, err := u.store.Stat(ctx, statesync.ArchiveKey(u.chainID, height))
	if errors.Is(err, statesync.ErrObjectNotFound) {
		return false, nil
	}
	return err == nil, err
}

// stage hard-links the checkpoint at height into the staging dir and reads its state sync proof
// file. It returns an error wrapping errCheckpointPruned when FlatKV removed either first.
func (u *stateSyncUploader) stage(height int64) ([]byte, error) {
	if err := os.RemoveAll(u.stagingDir); err != nil {
		return nil, fmt.Errorf("remove earlier upload staging: %w", err)
	}
	if err := flatkv.HardLinkSnapshot(u.flatKVDir, height, u.stagingDir); err != nil {
		return nil, prunedIfNotExist(err)
	}
	proof, err := os.ReadFile(u.proofPath(height))
	if err != nil {
		return nil, prunedIfNotExist(fmt.Errorf("read state sync proof file: %w", err))
	}
	return proof, nil
}

// prunedIfNotExist wraps err with errCheckpointPruned when err reports a missing file.
func prunedIfNotExist(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %w", errCheckpointPruned, err)
	}
	return err
}

// putArchive streams the archive of m from the staged checkpoint to the store. An archive that the
// store already has at the key is kept, and putArchive returns nil.
func (u *stateSyncUploader) putArchive(ctx context.Context, m *statesync.Manifest, proof []byte, size int64) error {
	pr, pw := io.Pipe()
	written := make(chan error, 1)
	go func() {
		err := statesync.WriteArchive(ctx, pw, m, proof, u.stagingDir)
		_ = pw.CloseWithError(err)
		written <- err
	}()
	key := statesync.ArchiveKey(u.chainID, m.Height)
	putErr := u.store.Put(ctx, key, pr, size, statesync.Precondition{Absent: true})
	_ = pr.CloseWithError(errUploadEnded)
	writeErr := <-written
	if errors.Is(putErr, statesync.ErrPreconditionFailed) {
		logger.Info("state sync archive is already in the store", "key", key)
		return nil
	}
	if putErr != nil {
		return putErr
	}
	return writeErr
}

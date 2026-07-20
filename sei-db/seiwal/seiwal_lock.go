package seiwal

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/zbiljic/go-filelock"

	commonerrors "github.com/sei-protocol/sei-chain/sei-db/common/errors"
)

// The name of the WAL directory lock file. Holding this lock grants exclusive access to the WAL directory,
// so a second WAL instance or an offline utility cannot mutate the directory while another owner is live.
const lockFileName = "wal.lock"

// acquireDirLock takes an exclusive advisory lock on dir/wal.lock. It fails with
// commonerrors.ErrFileLockUnavailable if another WAL instance or offline utility already holds the lock. The
// caller owns the returned handle and must Unlock it to release the directory.
func acquireDirLock(dir string) (filelock.TryLockerSafe, error) {
	lockPath, err := filepath.Abs(filepath.Join(dir, lockFileName))
	if err != nil {
		return nil, fmt.Errorf("resolve lock path: %w", err)
	}
	fl, err := filelock.New(lockPath)
	if err != nil {
		return nil, fmt.Errorf("create file lock %s: %w", lockPath, err)
	}
	locked, err := fl.TryLock()
	if err != nil {
		if errors.Is(err, filelock.ErrLocked) {
			return nil, fmt.Errorf("%w: %s", commonerrors.ErrFileLockUnavailable, lockPath)
		}
		return nil, fmt.Errorf("acquire file lock %s: %w", lockPath, err)
	}
	if !locked {
		return nil, fmt.Errorf("%w: held by another owner (%s)", commonerrors.ErrFileLockUnavailable, lockPath)
	}
	return fl, nil
}

// releaseDirLock releases a lock acquired by acquireDirLock, logging any failure. A release failure is not
// fatal: the lock is advisory and the kernel drops it when the process exits.
func releaseDirLock(lock filelock.TryLockerSafe, dir string) {
	if err := lock.Unlock(); err != nil {
		logger.Error("failed to release WAL directory lock", "path", dir, "error", err)
	}
}

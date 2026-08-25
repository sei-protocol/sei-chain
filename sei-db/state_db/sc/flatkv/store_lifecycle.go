package flatkv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// isClosed reports whether the store's databases have been released. The stores own them, so an open
// store is one that still has stores.
func (s *CommitStore) isClosed() bool {
	return s.stores == nil
}

// closeDBsOnly closes the stores, and with them the databases they own, while retaining the file lock —
// which prevents a race window during Rollback or LoadVersion.
//
// It deliberately does NOT close the WAL: the injected WAL's lifecycle is decoupled from the DB
// open/close cycle and must survive the reopen that Rollback/LoadVersion perform. The WAL is closed
// only by top-level Close, or replaced in place by Rollback/restore.
func (s *CommitStore) closeDBsOnly() error {
	if err := s.closeStores(); err != nil {
		return fmt.Errorf("stores close: %w", err)
	}
	s.localMeta = make(map[string]*ktype.LocalMeta)
	return nil
}

// Close drains thread pools, closes all database instances, cancels the store's context to stop background
// goroutines (caches, metrics), and releases the file lock.
//
// Close does not coordinate with concurrent operations: it releases resources they still hold, and no lock
// guards them against it. Callers need not quiesce first — a background export replaying this store's WAL into
// a read-only clone (see replayIntoReadOnlyCopy) ends with a closed-WAL error.
//
// That overlap includes an unsynchronized read of the WAL's closed flag. The racing read either observes the
// flag or falls through to the WAL's own closed check, so it changes nothing a caller can see, but a test that
// closes the store while an export runs would trip the race detector.
func (s *CommitStore) Close() error {
	// Stores before pools, and pools before databases. The stores' lifecycle goroutines flush through
	// the databases, and a database's own cache layer submits its writes to miscPool, so closing the
	// pools while a store is still flushing panics with "submit on closed pool". Store Close does not
	// return until no store-owned goroutine will touch the database again, which is exactly the
	// guarantee that makes the rest of this teardown safe.
	var storeErr error
	if err := s.closeStores(); err != nil {
		storeErr = fmt.Errorf("stores close: %w", err)
	}

	if s.readPool != nil {
		s.readPool.Close()
		s.readPool = nil
	}
	if s.miscPool != nil {
		s.miscPool.Close()
		s.miscPool = nil
	}
	if s.ltHashPool != nil {
		s.ltHashPool.Close()
		s.ltHashPool = nil
	}
	if s.sortPool != nil {
		s.sortPool.Close()
		s.sortPool = nil
	}
	// Calculator is bound to ltHashPool; drop it so a post-Close use cannot
	// submit to a closed pool. resetPools recreates both together.
	s.ltCalc = nil

	err := errors.Join(storeErr, s.closeDBsOnly())

	// FlatKV owns Close of whatever WAL instance it currently holds (the injected one, or a replacement made
	// by rollback/restore). A nil WAL means the outer context owns the pipeline — nothing to close. The
	// closed instance is deliberately retained rather than nilled: a store constructed with a WAL holds one
	// for its whole life, so a later write fails loudly against the closed WAL instead of being silently
	// skipped as it would be against a nil one.
	if s.wal != nil {
		if walErr := s.wal.Close(); walErr != nil {
			err = errors.Join(err, fmt.Errorf("WAL close: %w", walErr))
		}
	}

	s.cancel()

	if s.fileLock != nil {
		if lockErr := s.fileLock.Unlock(); lockErr != nil {
			err = errors.Join(err, fmt.Errorf("file lock release: %w", lockErr))
		}
		s.fileLock = nil
	}

	if s.readOnlyWorkDir != "" {
		if rmErr := os.RemoveAll(s.readOnlyWorkDir); rmErr != nil {
			err = errors.Join(err, fmt.Errorf("remove readonly workdir: %w", rmErr))
		}
	}

	if err != nil {
		return err
	}

	logger.Info("FlatKV store closed")
	return nil
}

// CleanupOrphanedReadOnlyDirs acquires the writer lock and removes readonly-*
// working directories left behind by a previous process crash. It is a
// startup-only API and must be called before any read-only instances are
// created in the current process. The acquired writer lock is retained for
// subsequent LoadLatest calls.
func (s *CommitStore) CleanupOrphanedReadOnlyDirs() error {
	dir := s.flatkvDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create flatkv dir: %w", err)
	}
	if s.fileLock == nil {
		if err := s.acquireFileLock(dir); err != nil {
			return err
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read flatkv dir: %w", err)
	}
	var errs []error
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), readOnlyDirPrefix) {
			logger.Info("removing orphaned readonly dir", "dir", e.Name())
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				errs = append(errs, fmt.Errorf("remove orphaned dir %s: %w", e.Name(), err))
			}
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Exporter creates an exporter for the given version by opening a read-only
// clone and performing a full scan of all DBs. The returned exporter must be
// closed when done (which also closes the read-only clone).
func (s *CommitStore) Exporter(version int64) (types.Exporter, error) {
	if s.readOnly {
		return nil, errReadOnly
	}
	roStore, err := s.LoadVersionReadOnly(version)
	if err != nil {
		return nil, fmt.Errorf("load readonly version for export: %w", err)
	}
	cs, ok := roStore.(*CommitStore)
	if !ok {
		_ = roStore.Close()
		return nil, fmt.Errorf("unexpected store type from LoadVersionReadOnly")
	}
	return NewKVExporter(cs, version), nil
}

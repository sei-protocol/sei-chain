package flatkv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// isClosed reports whether the store's DB handles have been released.
func (s *CommitStore) isClosed() bool {
	return s.metadataDB == nil && s.accountDB == nil &&
		s.codeDB == nil && s.storageDB == nil && s.legacyDB == nil
}

// closeDBsOnly closes all database handles and the WAL but retains the
// file lock, preventing a race window during Rollback or LoadVersion.
func (s *CommitStore) closeDBsOnly() error {
	var errs []error

	if s.changelog != nil {
		if err := s.changelog.Close(); err != nil {
			errs = append(errs, fmt.Errorf("changelog close: %w", err))
		}
	}
	s.changelog = nil

	if s.metadataDB != nil {
		if err := s.metadataDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("metadataDB close: %w", err))
		}
		s.metadataDB = nil
	}

	if s.storageDB != nil {
		if err := s.storageDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("storageDB close: %w", err))
		}
		s.storageDB = nil
	}
	if s.codeDB != nil {
		if err := s.codeDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("codeDB close: %w", err))
		}
		s.codeDB = nil
	}
	if s.accountDB != nil {
		if err := s.accountDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("accountDB close: %w", err))
		}
		s.accountDB = nil
	}

	if s.legacyDB != nil {
		if err := s.legacyDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("legacyDB close: %w", err))
		}
		s.legacyDB = nil
	}

	s.localMeta = make(map[string]*LocalMeta)

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Close closes all database instances, cancels the store's context to
// stop background goroutines (pools, caches, metrics), and releases the
// file lock.
func (s *CommitStore) Close() error {
	err := s.closeDBsOnly()
	s.cancel()

	if s.fileLock != nil {
		if lockErr := s.fileLock.Unlock(); lockErr != nil {
			err = errors.Join(err, fmt.Errorf("file lock release: %w", lockErr))
		}
		s.fileLock = nil
	}

	if s.readOnlyWorkDir != "" {
		_ = os.RemoveAll(s.readOnlyWorkDir)
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
// subsequent LoadVersion(..., false) calls.
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
		return nil
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), readOnlyDirPrefix) {
			logger.Info("removing orphaned readonly dir", "dir", e.Name())
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
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
	roStore, err := s.LoadVersion(version, true)
	if err != nil {
		return nil, fmt.Errorf("load readonly version for export: %w", err)
	}
	cs, ok := roStore.(*CommitStore)
	if !ok {
		_ = roStore.Close()
		return nil, fmt.Errorf("unexpected store type from LoadVersion")
	}
	return NewKVExporter(cs, version), nil
}

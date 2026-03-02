package flatkv

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// closeDBsOnly closes all database handles and the WAL but retains the
// file lock, preventing a race window during Rollback or LoadVersion.
func (s *CommitStore) closeDBsOnly() error {
	var errs []error

	if s.changelog != nil {
		if err := s.changelog.Close(); err != nil {
			errs = append(errs, fmt.Errorf("changelog close: %w", err))
		}
		s.changelog = nil
	}

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

// Close closes all database instances and releases the file lock.
func (s *CommitStore) Close() error {
	err := s.closeDBsOnly()

	if s.fileLock != nil {
		if lockErr := s.fileLock.Unlock(); lockErr != nil {
			err = errors.Join(err, fmt.Errorf("file lock release: %w", lockErr))
		}
		s.fileLock = nil
	}

	if err != nil {
		return err
	}

	s.log.Info("FlatKV store closed")
	return nil
}

// Exporter creates an exporter for the given version.
// NOTE: Not yet implemented. Will be added with state-sync support.
// The future implementation will export each DB separately with internal key format.
func (s *CommitStore) Exporter(version int64) (types.Exporter, error) {
	return nil, fmt.Errorf("not implemented")
}

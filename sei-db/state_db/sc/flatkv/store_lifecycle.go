package flatkv

import (
	"errors"
	"fmt"
)

// Close closes all database instances.
func (s *CommitStore) Close() error {
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

	if s.fileLock != nil {
		if err := s.fileLock.Unlock(); err != nil {
			errs = append(errs, fmt.Errorf("file lock release: %w", err))
		}
		s.fileLock = nil
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	s.log.Info("FlatKV store closed")
	return nil
}

// Exporter creates an exporter for the given version.
// NOTE: Not yet implemented. Will be added with state-sync support.
// The future implementation will export each DB separately with internal key format.
func (s *CommitStore) Exporter(version int64) (Exporter, error) {
	return &notImplementedExporter{}, nil
}
